package artifactcache

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

type cleanupGroupRef struct{ alias, repository, key string }

type cleanupPlanner struct {
	ecosystemName  string
	groupKey       func(string, string) string
	groups         map[cleanupGroupRef]*ecosystem.CacheCleanupGroup
	manifests      map[string][]meta.ManifestRecord
	matched        map[string]bool
	refs           map[string]cleanupGroupRef
	groupsByDigest map[string][]cleanupGroupRef
	ownersByDigest map[string][]cleanupGroupRef
	foreignDigests map[string]bool
}

// PlanCacheCleanup groups an ecosystem's metadata records into logical cache units.
// It returns every group; the scheduler decides which are stale and eligible for deletion.
func PlanCacheCleanup(ctx context.Context, ecosystemName string, inventory ecosystem.CacheCleanupInventory, _ ecosystem.CacheCleanupOptions, groupKey func(repository, reference string) string) ([]ecosystem.CacheCleanupGroup, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("plan %s cache cleanup: %w", ecosystemName, err)
	}
	if groupKey == nil || strings.TrimSpace(ecosystemName) == "" {
		return nil, nil
	}
	planner := newCleanupPlanner(ecosystemName, groupKey)
	planner.indexManifests(inventory.Manifests)
	planner.groupTags(inventory.Tags)
	planner.groupUntaggedManifests()
	planner.applyBlobAccess(inventory.Blobs)
	planner.applyRepoBlobAccess(inventory.RepoBlobs)
	planner.applyPullAccess(inventory.Pulls)
	return planner.result(), nil
}

func newCleanupPlanner(ecosystemName string, groupKey func(string, string) string) *cleanupPlanner {
	return &cleanupPlanner{
		ecosystemName:  ecosystemName,
		groupKey:       groupKey,
		groups:         make(map[cleanupGroupRef]*ecosystem.CacheCleanupGroup),
		manifests:      make(map[string][]meta.ManifestRecord),
		matched:        make(map[string]bool),
		refs:           make(map[string]cleanupGroupRef),
		groupsByDigest: make(map[string][]cleanupGroupRef),
		ownersByDigest: make(map[string][]cleanupGroupRef),
		foreignDigests: make(map[string]bool),
	}
}

func (p *cleanupPlanner) indexManifests(records []meta.ManifestRecord) {
	for recordIndex := range records {
		record := records[recordIndex]
		if record.AcceptKey != "" && record.AcceptKey != p.ecosystemName {
			p.foreignDigests[record.Digest] = true
			continue
		}
		if record.AcceptKey == "" && !strings.HasPrefix(record.Alias, p.ecosystemName+"/") {
			p.foreignDigests[record.Digest] = true
			continue
		}
		key := cleanupIdentity(record.Alias, record.Repository, record.Digest)
		p.manifests[key] = append(p.manifests[key], record)
	}
}

func (p *cleanupPlanner) groupTags(tags []meta.TagRecord) {
	for tagIndex := range tags {
		tag := tags[tagIndex]
		key := cleanupIdentity(tag.Alias, tag.Repository, tag.Digest)
		for manifestIndex := range p.manifests[key] {
			manifest := p.manifests[key][manifestIndex]
			ref := p.groupFor(tag.Alias, tag.Repository, tag.Reference)
			group := p.groups[ref]
			group.Tags = append(group.Tags, tag)
			group.Manifests = append(group.Manifests, manifest)
			group.Digests = append(group.Digests, manifest.Digest)
			p.registerDigestOwner(ref, manifest.Digest)
			p.matched[key] = true
			p.refs[cleanupIdentity(tag.Alias, tag.Repository, tag.Reference)] = ref
			updateCleanupActivity(&group.LastAccessAt, latestTime(tag.UpdatedAt, tag.CreatedAt))
			if manifest.Reference == tag.Reference {
				updateCleanupActivity(&group.LastAccessAt, latestTime(manifest.UpdatedAt, manifest.CreatedAt))
			}
		}
	}
}

func (p *cleanupPlanner) groupUntaggedManifests() {
	for key, records := range p.manifests {
		if p.matched[key] {
			continue
		}
		for manifestIndex := range records {
			manifest := records[manifestIndex]
			reference := manifest.Reference
			if reference == "" {
				reference = manifest.Digest
			}
			ref := p.groupFor(manifest.Alias, manifest.Repository, reference)
			group := p.groups[ref]
			group.Manifests = append(group.Manifests, manifest)
			group.Digests = append(group.Digests, manifest.Digest)
			p.registerDigestOwner(ref, manifest.Digest)
			p.refs[cleanupIdentity(manifest.Alias, manifest.Repository, reference)] = ref
			updateCleanupActivity(&group.LastAccessAt, manifest.UpdatedAt)
		}
	}
}

func (p *cleanupPlanner) registerDigestOwner(ref cleanupGroupRef, digest string) {
	key := cleanupIdentity(ref.alias, ref.repository, digest)
	if !slices.Contains(p.groupsByDigest[key], ref) {
		p.groupsByDigest[key] = append(p.groupsByDigest[key], ref)
	}
	if !slices.Contains(p.ownersByDigest[digest], ref) {
		p.ownersByDigest[digest] = append(p.ownersByDigest[digest], ref)
	}
}

func (p *cleanupPlanner) groupFor(alias, repository, reference string) cleanupGroupRef {
	key := p.groupKey(repository, reference)
	ref := cleanupGroupRef{alias: alias, repository: repository, key: key}
	if p.groups[ref] == nil {
		p.groups[ref] = &ecosystem.CacheCleanupGroup{Ecosystem: p.ecosystemName, Alias: alias, Package: key}
	}
	return ref
}

func (p *cleanupPlanner) applyBlobAccess(blobs []meta.BlobRecord) {
	accessByDigest := make(map[string]time.Time, len(blobs))
	for blobIndex := range blobs {
		blob := blobs[blobIndex]
		access := accessByDigest[blob.Digest]
		updateCleanupActivity(&access, latestTime(blob.LastAccessAt, blob.UpdatedAt, blob.CreatedAt))
		accessByDigest[blob.Digest] = access
	}
	for _, group := range p.groups {
		for _, digest := range group.Digests {
			if p.foreignDigests[digest] || len(p.ownersByDigest[digest]) != 1 {
				continue
			}
			updateCleanupActivity(&group.LastAccessAt, accessByDigest[digest])
		}
	}
}

func (p *cleanupPlanner) applyRepoBlobAccess(records []meta.RepoBlobRecord) {
	for recordIndex := range records {
		record := records[recordIndex]
		key := cleanupIdentity(record.Alias, record.Repository, record.Digest)
		for _, ref := range p.groupsByDigest[key] {
			group := p.groups[ref]
			group.RepoBlobs = append(group.RepoBlobs, record)
			if !p.foreignDigests[record.Digest] && len(p.ownersByDigest[record.Digest]) == 1 {
				updateCleanupActivity(&group.LastAccessAt, latestTime(record.LastAccessAt, record.LastVerifiedAt, record.UpdatedAt, record.CreatedAt))
			}
		}
	}
}

func (p *cleanupPlanner) applyPullAccess(records []meta.PullRecord) {
	for recordIndex := range records {
		record := records[recordIndex]
		alias := strings.TrimPrefix(record.Alias, p.ecosystemName+"/")
		if ref, ok := p.refs[cleanupIdentity(alias, record.Repository, record.Reference)]; ok {
			updateCleanupActivity(&p.groups[ref].LastAccessAt, latestTime(record.LastPullAt, record.LastUpstreamPullAt, record.UpdatedAt, record.CreatedAt))
		}
	}
}

func (p *cleanupPlanner) result() []ecosystem.CacheCleanupGroup {
	result := make([]ecosystem.CacheCleanupGroup, 0, len(p.groups))
	for _, group := range p.groups {
		group.Digests = uniqueStrings(group.Digests)
		result = append(result, *group)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Alias != result[j].Alias {
			return result[i].Alias < result[j].Alias
		}
		if result[i].Package != result[j].Package {
			return result[i].Package < result[j].Package
		}
		return result[i].Ecosystem < result[j].Ecosystem
	})
	return result
}

func cleanupIdentity(alias, repository, reference string) string {
	return alias + "\x00" + repository + "\x00" + reference
}

func updateCleanupActivity(current *time.Time, candidate time.Time) {
	if candidate.After(*current) {
		*current = candidate
	}
}

func latestTime(candidates ...time.Time) time.Time {
	var latest time.Time
	for _, candidate := range candidates {
		updateCleanupActivity(&latest, candidate)
	}
	return latest
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
