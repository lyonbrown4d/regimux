package prefetch

import (
	"context"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/cache"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const maxManifestPrefetchDepth = 1

type prefetchResult struct {
	manifestDigest     string
	layerCount         int
	blobCount          int
	childManifestCount int
	bytesWarmed        int64
}

type blobDescriptor struct {
	digest    string
	mediaType string
	size      int64
	kind      string
}

type manifestMediaEnvelope struct {
	MediaType string `json:"mediaType"`
}

func (r prefetchResult) add(next prefetchResult) prefetchResult {
	if r.manifestDigest == "" {
		r.manifestDigest = next.manifestDigest
	}
	r.layerCount += next.layerCount
	r.blobCount += next.blobCount
	r.childManifestCount += next.childManifestCount
	r.bytesWarmed += next.bytesWarmed
	return r
}

func (s *Service) prefetchManifestArtifacts(
	ctx context.Context,
	opts RunOptions,
	execution *runExecution,
	candidate Candidate,
	reference string,
	manifest *cache.CachedManifest,
	depth int,
) (prefetchResult, error) {
	result := prefetchResult{manifestDigest: cachedManifestDigest(manifest, reference)}
	mediaType := cachedManifestMediaType(manifest)
	switch {
	case isImageManifestMediaType(mediaType):
		if opts.ManifestOnly {
			return result, nil
		}
		next, err := s.prefetchImageManifestBlobs(ctx, execution, candidate, manifest)
		return result.add(next), err
	case isIndexManifestMediaType(mediaType):
		next, err := s.prefetchIndexManifests(ctx, opts, execution, candidate, manifest, depth)
		return result.add(next), err
	case mediaType == "":
		s.logSkippedManifest(ctx, candidate, reference, result.manifestDigest, mediaType, "empty media type")
	default:
		s.logSkippedManifest(ctx, candidate, reference, result.manifestDigest, mediaType, "unsupported media type")
	}
	return result, nil
}

func (s *Service) prefetchImageManifestBlobs(
	ctx context.Context,
	execution *runExecution,
	candidate Candidate,
	manifest *cache.CachedManifest,
) (prefetchResult, error) {
	if s.blobs == nil {
		return prefetchResult{}, cacheError("prefetch blob service is not configured")
	}
	descriptors, layerCount, err := imageManifestBlobDescriptors(manifest)
	result := prefetchResult{
		manifestDigest: cachedManifestDigest(manifest, ""),
		layerCount:     layerCount,
	}
	if err != nil {
		return result, err
	}
	plannedBytes := descriptorByteSize(descriptors)
	if !execution.reserveBytes(plannedBytes) {
		return result, errPrefetchBudgetExceeded
	}
	warmed, bytesWarmed, err := s.prefetchBlobDescriptors(ctx, candidate, result.manifestDigest, descriptors)
	result.blobCount = warmed
	result.bytesWarmed = bytesWarmed
	return result, err
}

func (s *Service) prefetchIndexManifests(
	ctx context.Context,
	opts RunOptions,
	execution *runExecution,
	candidate Candidate,
	manifest *cache.CachedManifest,
	depth int,
) (prefetchResult, error) {
	result := prefetchResult{manifestDigest: cachedManifestDigest(manifest, "")}
	if depth >= maxManifestPrefetchDepth {
		s.logSkippedManifest(ctx, candidate, "", result.manifestDigest, cachedManifestMediaType(manifest), "maximum manifest depth reached")
		return result, nil
	}

	children, err := imageIndexDescriptors(manifest)
	if err != nil {
		return result, err
	}
	for i := range children {
		child := children[i]
		if err := ctx.Err(); err != nil {
			return result, cacheWrap(err, "prefetch child manifest context")
		}
		if !usableManifestDescriptor(child) {
			s.logSkippedManifest(ctx, candidate, string(child.Digest), string(child.Digest), child.MediaType, "unsupported child manifest descriptor")
			continue
		}
		childResult, err := s.prefetchChildManifest(ctx, opts, execution, candidate, child, depth)
		result.childManifestCount++
		result = result.add(childResult)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func (s *Service) prefetchChildManifest(
	ctx context.Context,
	opts RunOptions,
	execution *runExecution,
	candidate Candidate,
	child ocispec.Descriptor,
	depth int,
) (prefetchResult, error) {
	reference := string(child.Digest)
	manifest, err := s.manifests.Get(ctx, cache.ManifestRequest{
		UpstreamAlias:  candidate.Alias,
		Repo:           candidate.Repo,
		Reference:      reference,
		Accept:         opts.Accept,
		Method:         http.MethodGet,
		SkipPullRecord: true,
	})
	if err != nil {
		return prefetchResult{manifestDigest: reference}, cacheWrap(err, "prefetch child manifest")
	}
	return s.prefetchManifestArtifacts(ctx, opts, execution, candidate, reference, manifest, depth+1)
}

func (s *Service) prefetchBlobDescriptors(
	ctx context.Context,
	candidate Candidate,
	manifestDigest string,
	descriptors []blobDescriptor,
) (int, int64, error) {
	warmed := 0
	bytesWarmed := int64(0)
	for i := range descriptors {
		if err := ctx.Err(); err != nil {
			return warmed, bytesWarmed, cacheWrap(err, "prefetch blob context")
		}
		descriptor := descriptors[i]
		if descriptor.digest == "" {
			s.logSkippedBlob(ctx, candidate, manifestDigest, descriptor, "empty digest")
			continue
		}
		size, err := s.prefetchBlob(ctx, candidate, manifestDigest, descriptor)
		if err != nil {
			return warmed, bytesWarmed, err
		}
		warmed++
		bytesWarmed += size
	}
	return warmed, bytesWarmed, nil
}

func (s *Service) prefetchBlob(
	ctx context.Context,
	candidate Candidate,
	manifestDigest string,
	descriptor blobDescriptor,
) (int64, error) {
	result, err := s.blobs.Get(ctx, cache.BlobRequest{
		UpstreamAlias: candidate.Alias,
		Repo:          candidate.Repo,
		Digest:        descriptor.digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "prefetch blob failed",
			"alias", candidate.Alias,
			"repository", candidate.Repo,
			"reference", candidate.Tag,
			"manifest_digest", manifestDigest,
			"digest", descriptor.digest,
			"media_type", descriptor.mediaType,
			"size", descriptor.size,
			"kind", descriptor.kind,
			"error", err,
		)
		return 0, cacheWrap(err, "prefetch blob")
	}
	if err := closePrefetchBlob(result); err != nil {
		return 0, err
	}
	size := descriptor.size
	if size <= 0 {
		size = result.Size
	}
	return size, nil
}

func (s *Service) logSkippedManifest(ctx context.Context, candidate Candidate, reference, digest, mediaType, reason string) {
	s.logger.DebugContext(ctx, "skipped prefetch manifest artifact",
		"alias", candidate.Alias,
		"repository", candidate.Repo,
		"reference", reference,
		"digest", digest,
		"media_type", mediaType,
		"reason", reason,
	)
}

func (s *Service) logSkippedBlob(ctx context.Context, candidate Candidate, manifestDigest string, descriptor blobDescriptor, reason string) {
	s.logger.DebugContext(ctx, "skipped prefetch blob",
		"alias", candidate.Alias,
		"repository", candidate.Repo,
		"reference", candidate.Tag,
		"manifest_digest", manifestDigest,
		"digest", descriptor.digest,
		"media_type", descriptor.mediaType,
		"size", descriptor.size,
		"kind", descriptor.kind,
		"reason", reason,
	)
}

func descriptorByteSize(descriptors []blobDescriptor) int64 {
	total := int64(0)
	for i := range descriptors {
		if descriptors[i].size > 0 {
			total += descriptors[i].size
		}
	}
	return total
}
