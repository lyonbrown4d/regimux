package meta

import (
	"strings"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/reference"
)

func normalizeEndpointHealthRecord(record EndpointHealthRecord) (EndpointHealthKey, EndpointHealthRecord, error) {
	key, err := normalizeEndpointHealthKey(EndpointHealthKey{
		Alias:      record.Alias,
		Registry:   record.Registry,
		Repository: record.Repository,
	})
	if err != nil {
		return EndpointHealthKey{}, EndpointHealthRecord{}, err
	}
	if record.LatencyEWMA < 0 {
		return EndpointHealthKey{}, EndpointHealthRecord{}, errorf("%w: endpoint health latency cannot be negative", ErrInvalidValue)
	}
	if record.LatencySamples < 0 {
		return EndpointHealthKey{}, EndpointHealthRecord{}, errorf("%w: endpoint health latency samples cannot be negative", ErrInvalidValue)
	}
	if record.ConsecutiveFailures < 0 {
		return EndpointHealthKey{}, EndpointHealthRecord{}, errorf("%w: endpoint health consecutive failures cannot be negative", ErrInvalidValue)
	}
	if record.SuccessCount < 0 || record.FailureCount < 0 || record.ContentMismatchCount < 0 {
		return EndpointHealthKey{}, EndpointHealthRecord{}, errorf("%w: endpoint health counters cannot be negative", ErrInvalidValue)
	}
	record.Key = key.String()
	record.Alias = key.Alias
	record.Registry = key.Registry
	record.Repository = key.Repository
	return key, record, nil
}

func normalizeEndpointHealthKey(key EndpointHealthKey) (EndpointHealthKey, error) {
	alias, err := required("alias", key.Alias)
	if err != nil {
		return EndpointHealthKey{}, err
	}
	registry := strings.TrimRight(strings.TrimSpace(key.Registry), "/")
	if registry == "" {
		return EndpointHealthKey{}, errorf("%w: endpoint health registry is required", ErrInvalidKey)
	}
	return EndpointHealthKey{
		Alias:      alias,
		Registry:   registry,
		Repository: normalizeRepository(key.Repository),
	}, nil
}

func normalizeUpstreamAlias(alias string) (string, error) {
	return required("alias", alias)
}

func normalizeRepositoryName(name string) (string, error) {
	return required("repository", name)
}

func repositoryMetadataKey(alias, name string) string {
	return alias + "/" + name
}

func normalizeManifestRecord(record ManifestRecord) (ManifestKey, ManifestRecord, error) {
	key, err := normalizeManifestKey(ManifestKey{
		Alias:      record.Alias,
		Repository: record.Repository,
		Digest:     record.Digest,
	})
	if err != nil {
		return ManifestKey{}, ManifestRecord{}, err
	}
	if record.Size < 0 {
		return ManifestKey{}, ManifestRecord{}, errorf("%w: manifest size cannot be negative", ErrInvalidValue)
	}
	record.Alias = key.Alias
	record.Repository = key.Repository
	record.Digest = key.Digest
	record.Key = key.String()
	record.Headers = cloneHeaders(record.Headers)
	return key, record, nil
}

func normalizeManifestKey(key ManifestKey) (ManifestKey, error) {
	alias, err := required("alias", key.Alias)
	if err != nil {
		return ManifestKey{}, err
	}
	repo, err := required("repository", key.Repository)
	if err != nil {
		return ManifestKey{}, err
	}
	digest, err := normalizeDigest(key.Digest)
	if err != nil {
		return ManifestKey{}, err
	}
	return ManifestKey{Alias: alias, Repository: repo, Digest: digest}, nil
}

func normalizeTagRecord(record TagRecord) (TagKey, TagRecord, error) {
	key, err := normalizeTagKey(TagKey{
		Alias:      record.Alias,
		Repository: record.Repository,
		Reference:  record.Reference,
	})
	if err != nil {
		return TagKey{}, TagRecord{}, err
	}
	digest, err := normalizeDigest(record.Digest)
	if err != nil {
		return TagKey{}, TagRecord{}, err
	}
	record.Alias = key.Alias
	record.Repository = key.Repository
	record.Reference = key.Reference
	record.Digest = digest
	record.Key = key.String()
	return key, record, nil
}

func normalizeTagKey(key TagKey) (TagKey, error) {
	alias, err := required("alias", key.Alias)
	if err != nil {
		return TagKey{}, err
	}
	repo, err := required("repository", key.Repository)
	if err != nil {
		return TagKey{}, err
	}
	ref, err := required("reference", key.Reference)
	if err != nil {
		return TagKey{}, err
	}
	return TagKey{Alias: alias, Repository: repo, Reference: ref}, nil
}

func normalizePullKey(key PullKey) (PullKey, error) {
	alias, err := required("alias", key.Alias)
	if err != nil {
		return PullKey{}, err
	}
	repo, err := required("repository", key.Repository)
	if err != nil {
		return PullKey{}, err
	}
	ref, err := required("reference", key.Reference)
	if err != nil {
		return PullKey{}, err
	}
	return PullKey{Alias: alias, Repository: repo, Reference: ref}, nil
}

func normalizeBlobRecord(record BlobRecord) (BlobKey, BlobRecord, error) {
	key, err := normalizeBlobKey(BlobKey{Digest: record.Digest})
	if err != nil {
		return BlobKey{}, BlobRecord{}, err
	}
	if record.Size < 0 {
		return BlobKey{}, BlobRecord{}, errorf("%w: blob size cannot be negative", ErrInvalidValue)
	}
	record.Digest = key.Digest
	return key, record, nil
}

func normalizeRepoBlobRecord(record RepoBlobRecord) (RepoBlobKey, RepoBlobRecord, error) {
	key, err := normalizeRepoBlobKey(RepoBlobKey{
		Alias:      record.Alias,
		Repository: record.Repository,
		Digest:     record.Digest,
	})
	if err != nil {
		return RepoBlobKey{}, RepoBlobRecord{}, err
	}
	record.Alias = key.Alias
	record.Repository = key.Repository
	record.Digest = key.Digest
	record.Key = key.String()
	return key, record, nil
}

func normalizeRepoBlobKey(key RepoBlobKey) (RepoBlobKey, error) {
	alias, err := required("alias", key.Alias)
	if err != nil {
		return RepoBlobKey{}, err
	}
	repo, err := required("repository", key.Repository)
	if err != nil {
		return RepoBlobKey{}, err
	}
	digest, err := normalizeDigest(key.Digest)
	if err != nil {
		return RepoBlobKey{}, err
	}
	return RepoBlobKey{Alias: alias, Repository: repo, Digest: digest}, nil
}

func normalizePrefetchCandidateKey(key PrefetchCandidateKey) (PrefetchCandidateKey, error) {
	alias, err := required("alias", key.Alias)
	if err != nil {
		return PrefetchCandidateKey{}, err
	}
	repo, err := required("repository", key.Repository)
	if err != nil {
		return PrefetchCandidateKey{}, err
	}
	ref, err := required("reference", key.Reference)
	if err != nil {
		return PrefetchCandidateKey{}, err
	}
	return PrefetchCandidateKey{Alias: alias, Repository: repo, Reference: ref}, nil
}

func normalizePrefetchOutcomeRecord(record PrefetchOutcomeRecord) (PrefetchCandidateKey, PrefetchOutcomeRecord, error) {
	key, err := normalizePrefetchCandidateKey(PrefetchCandidateKey{
		Alias:      record.Alias,
		Repository: record.Repository,
		Reference:  record.Reference,
	})
	if err != nil {
		return PrefetchCandidateKey{}, PrefetchOutcomeRecord{}, err
	}
	if record.RunID < 0 {
		return PrefetchCandidateKey{}, PrefetchOutcomeRecord{}, errorf("%w: prefetch run id cannot be negative", ErrInvalidValue)
	}
	if record.BytesWarmed < 0 {
		return PrefetchCandidateKey{}, PrefetchOutcomeRecord{}, errorf("%w: prefetch bytes warmed cannot be negative", ErrInvalidValue)
	}
	record.Alias = key.Alias
	record.Repository = key.Repository
	record.Reference = key.Reference
	record.CandidateKey = key.String()
	return key, record, nil
}

func normalizeBlobKey(key BlobKey) (BlobKey, error) {
	digest, err := normalizeDigest(key.Digest)
	if err != nil {
		return BlobKey{}, err
	}
	return BlobKey{Digest: digest}, nil
}

func required(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errorf("%w: %s is required", ErrInvalidKey, name)
	}
	return value, nil
}

func normalizeRepository(value string) string {
	return strings.Trim(strings.TrimSpace(value), "/")
}

func normalizeDigest(value string) (string, error) {
	digest, err := reference.NormalizeDigest(value)
	if err != nil {
		return "", errorf("%w: %w", ErrInvalidKey, err)
	}
	return digest, nil
}

func cloneHeaders(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	out := collectionmapping.NewMapWithCapacity[string, []string](len(headers))
	collectionmapping.NewMapFrom(headers).Range(func(key string, values []string) bool {
		out.Set(key, append([]string(nil), values...))
		return true
	})
	return out.All()
}
