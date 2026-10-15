package container

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

type cleanupIndex struct {
	manifestsByRepoHash map[string][]meta.ManifestRecord
	manifestsByDigest   map[string][]meta.ManifestRecord
	tagsByImage         map[string][]meta.TagRecord
	pullsByReference    map[string][]meta.PullRecord
	allRepoBlobs        []meta.RepoBlobRecord
}

// PlanCacheCleanup groups a tagged image with its complete OCI descriptor graph.
// Group activity is based on root tags, manifests, and pulls; shared layer
// activity does not keep every image that happens to use that layer alive.
func (r *Runtime) PlanCacheCleanup(
	ctx context.Context,
	inventory ecosystem.CacheCleanupInventory,
	_ ecosystem.CacheCleanupOptions,
) ([]ecosystem.CacheCleanupGroup, error) {
	if err := validateCleanupPlan(ctx, r); err != nil {
		return nil, err
	}
	index := indexContainerInventory(inventory)
	groups := make([]ecosystem.CacheCleanupGroup, 0, len(index.tagsByImage)+len(inventory.Manifests))
	for _, key := range sortedKeys(index.tagsByImage) {
		if err := planContextErr(ctx); err != nil {
			return nil, err
		}
		group, err := r.planTaggedImage(ctx, key, index.tagsByImage[key], index)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	untaggedGroups, err := r.planUntaggedManifests(ctx, inventory, index, ownedCleanupManifests(groups))
	if err != nil {
		return nil, err
	}
	groups = append(groups, untaggedGroups...)
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Alias != groups[j].Alias {
			return groups[i].Alias < groups[j].Alias
		}
		return groups[i].Package < groups[j].Package
	})
	return groups, nil
}

func validateCleanupPlan(ctx context.Context, runtime *Runtime) error {
	if ctx == nil {
		return errors.New("container cleanup planning context is required")
	}
	if err := planContextErr(ctx); err != nil {
		return err
	}
	if runtime == nil || runtime.cleanup == nil {
		return errors.New("container cleanup graph reader is not configured")
	}
	return nil
}

func planContextErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("container cleanup planning: %w", err)
	}
	return nil
}

func indexContainerInventory(inventory ecosystem.CacheCleanupInventory) cleanupIndex {
	index := cleanupIndex{
		manifestsByRepoHash: make(map[string][]meta.ManifestRecord),
		manifestsByDigest:   make(map[string][]meta.ManifestRecord),
		tagsByImage:         make(map[string][]meta.TagRecord),
		pullsByReference:    make(map[string][]meta.PullRecord),
		allRepoBlobs:        inventory.RepoBlobs,
	}
	for i := range inventory.Manifests {
		manifest := inventory.Manifests[i]
		if !isContainerManifest(manifest) {
			continue
		}
		key := repoDigest(manifest.Alias, manifest.Repository, manifest.Digest)
		index.manifestsByRepoHash[key] = append(index.manifestsByRepoHash[key], manifest)
		index.manifestsByDigest[manifest.Digest] = append(index.manifestsByDigest[manifest.Digest], manifest)
	}
	for i := range inventory.Tags {
		tag := inventory.Tags[i]
		if tag.Digest != "" {
			key := repoDigest(tag.Alias, tag.Repository, tag.Digest)
			if len(index.manifestsByRepoHash[key]) > 0 {
				index.tagsByImage[key] = append(index.tagsByImage[key], tag)
			}
		}
	}
	for i := range inventory.Pulls {
		pull := inventory.Pulls[i]
		key := refKey(pull.Alias, pull.Repository, pull.Reference)
		index.pullsByReference[key] = append(index.pullsByReference[key], pull)
	}
	return index
}

func isContainerManifest(manifest meta.ManifestRecord) bool {
	switch strings.ToLower(strings.TrimSpace(manifest.AcceptKey)) {
	case ecosystem.Go, ecosystem.Maven, ecosystem.NPM, ecosystem.PyPI, ecosystem.Dist:
		return false
	}
	if isOCIManifestMediaType(manifest.MediaType) || isOCIAcceptKey(manifest.AcceptKey) {
		return true
	}
	return manifest.AcceptKey == "" && strings.HasPrefix(manifest.Alias, ecosystem.Container+"/")
}

func isOCIManifestMediaType(value string) bool {
	switch distribution.NormalizeMediaType(value) {
	case distribution.MediaTypeOCIManifest, distribution.MediaTypeDockerManifest,
		distribution.MediaTypeOCIIndex, distribution.MediaTypeDockerManifestList:
		return true
	default:
		return false
	}
}

func isOCIAcceptKey(value string) bool {
	if len(value) != hex.EncodedLen(sha256.Size) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (r *Runtime) planTaggedImage(
	ctx context.Context,
	key string,
	tags []meta.TagRecord,
	index cleanupIndex,
) (ecosystem.CacheCleanupGroup, error) {
	alias, repository, rootDigest := splitRepoDigestKey(key)
	group := ecosystem.CacheCleanupGroup{
		Ecosystem: ecosystem.Container,
		Alias:     alias,
		Package:   repository + "@" + rootDigest,
		Tags:      append([]meta.TagRecord(nil), tags...),
	}
	group.LastAccessAt = imageTagActivity(group.LastAccessAt, tags, rootDigest, index.pullsByReference)
	seen := make(map[string]struct{})
	if err := r.addImageGraph(ctx, rootDigest, alias, repository, index, seen, &group); err != nil {
		return ecosystem.CacheCleanupGroup{}, err
	}
	group.RepoBlobs = repoBlobsForImage(index.allRepoBlobs, alias, repository, seen)
	return group, nil
}

func imageTagActivity(current time.Time, tags []meta.TagRecord, digest string, pulls map[string][]meta.PullRecord) time.Time {
	for i := range tags {
		tag := tags[i]
		current = latest(current, tag.UpdatedAt, tag.CreatedAt)
		for _, reference := range []string{tag.Reference, digest} {
			rows := pulls[refKey(tag.Alias, tag.Repository, reference)]
			for j := range rows {
				pull := rows[j]
				current = latest(current, pull.LastPullAt, pull.UpdatedAt, pull.CreatedAt)
			}
		}
	}
	return current
}

func (r *Runtime) addImageGraph(
	ctx context.Context,
	rootDigest, alias, repository string,
	index cleanupIndex,
	seen map[string]struct{},
	group *ecosystem.CacheCleanupGroup,
) error {
	queue := []string{rootDigest}
	for len(queue) > 0 {
		if err := planContextErr(ctx); err != nil {
			return err
		}
		digest := queue[0]
		queue = queue[1:]
		if _, ok := seen[digest]; ok {
			continue
		}
		seen[digest] = struct{}{}
		group.Digests = append(group.Digests, digest)
		manifests := graphManifests(digest, rootDigest, alias, repository, index)
		for i := range manifests {
			manifest := manifests[i]
			if err := r.appendGraphManifest(ctx, digest == rootDigest, manifest, group, &queue); err != nil {
				return err
			}
		}
	}
	return nil
}

func graphManifests(digest, rootDigest, alias, repository string, index cleanupIndex) []meta.ManifestRecord {
	if digest == rootDigest {
		return index.manifestsByRepoHash[repoDigest(alias, repository, digest)]
	}
	return index.manifestsByDigest[digest]
}

func (r *Runtime) appendGraphManifest(ctx context.Context, isRoot bool, manifest meta.ManifestRecord, group *ecosystem.CacheCleanupGroup, queue *[]string) error {
	group.Manifests = append(group.Manifests, manifest)
	if isRoot {
		group.LastAccessAt = latest(group.LastAccessAt, manifest.UpdatedAt, manifest.CreatedAt)
	}
	refs, err := r.cleanup.OCIReferences(ctx, manifest)
	if err != nil {
		return fmt.Errorf("read OCI references for %s: %w", manifest.Digest, err)
	}
	*queue = append(*queue, refs...)
	return nil
}

func repoBlobsForImage(
	repoBlobs []meta.RepoBlobRecord,
	alias, repository string,
	imageDigests map[string]struct{},
) []meta.RepoBlobRecord {
	result := make([]meta.RepoBlobRecord, 0)
	for i := range repoBlobs {
		row := repoBlobs[i]
		if row.Alias == alias && row.Repository == repository {
			if _, included := imageDigests[row.Digest]; included {
				result = append(result, row)
			}
		}
	}
	return result
}

var _ ecosystem.CacheCleanupPlanner = (*Runtime)(nil)
