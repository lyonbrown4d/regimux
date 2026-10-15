package cachegc

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

type PackageCleanupResult struct {
	Metadata          meta.Store
	Candidates        map[string]bool
	ScannedPackages   int
	EligiblePackages  int
	EvictedPackages   int
	RetainedPackages  int
	RetiredReferences int
	LimitReached      bool
}

// ApplyCleanupPlan retires complete package references chosen by the ecosystem
// SPIs. Physical objects are reclaimed separately, after all plans are combined.
func (s *Service) ApplyCleanupPlan(ctx context.Context, inventory ecosystem.CacheCleanupInventory, groups []ecosystem.CacheCleanupGroup, opts ecosystem.CacheCleanupOptions) (*PackageCleanupResult, error) {
	if opts.UnusedFor <= 0 || opts.MaxDeletes < 0 || opts.MaxScan < 0 {
		return nil, errors.New("invalid package cleanup retention or limits")
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	groups = slices.Clone(groups)
	slices.SortFunc(groups, compareCleanupGroups)
	result := &PackageCleanupResult{Metadata: s.metadata, Candidates: make(map[string]bool)}
	selected, err := s.selectPackages(ctx, inventory, groups, opts, result)
	if err != nil {
		return nil, err
	}
	addOrphanCandidates(inventory, groups, opts, result)
	retired := packageRetirements(inventory, groups, selected)
	retireOrphanLinks(inventory, opts, result.Candidates, &retired)
	if opts.DryRun {
		result.Metadata = &cleanupMetadataView{Store: s.metadata, inventory: inventory, retired: retired}
	} else if err := s.retirePackageMetadata(ctx, retired); err != nil {
		return nil, err
	}
	result.RetiredReferences = len(retired.tags) + len(retired.manifests) + len(retired.links)
	return result, nil
}

func compareCleanupGroups(a, b ecosystem.CacheCleanupGroup) int {
	if a.LastAccessAt.IsZero() != b.LastAccessAt.IsZero() {
		if a.LastAccessAt.IsZero() {
			return 1
		}
		return -1
	}
	if order := a.LastAccessAt.Compare(b.LastAccessAt); order != 0 {
		return order
	}
	return strings.Compare(a.Ecosystem+"\x00"+a.Alias+"\x00"+a.Package, b.Ecosystem+"\x00"+b.Alias+"\x00"+b.Package)
}

func (s *Service) selectPackages(ctx context.Context, inventory ecosystem.CacheCleanupInventory, groups []ecosystem.CacheCleanupGroup, opts ecosystem.CacheCleanupOptions, result *PackageCleanupResult) ([]bool, error) {
	selected := make([]bool, len(groups))
	blobs := make(map[string]meta.BlobRecord, len(inventory.Blobs))
	for i := range inventory.Blobs {
		blob := &inventory.Blobs[i]
		blobs[blob.Digest] = *blob
	}
	for i := range groups {
		group := &groups[i]
		if opts.MaxScan > 0 && result.ScannedPackages >= opts.MaxScan {
			result.LimitReached = true
			break
		}
		result.ScannedPackages++
		accepted, err := s.selectPackage(ctx, blobs, group, opts, result)
		if err != nil {
			return nil, err
		}
		selected[i] = accepted
	}
	result.RetainedPackages = len(groups) - result.EvictedPackages
	return selected, nil
}

func (s *Service) selectPackage(ctx context.Context, blobs map[string]meta.BlobRecord, group *ecosystem.CacheCleanupGroup, opts ecosystem.CacheCleanupOptions, result *PackageCleanupResult) (bool, error) {
	if group.LastAccessAt.IsZero() || !group.LastAccessAt.Before(opts.Now.Add(-opts.UnusedFor)) {
		return false, nil
	}
	result.EligiblePackages++
	if !packageFitsBudget(group.Digests, opts.MaxDeletes, result) {
		result.LimitReached = true
		return false, nil
	}
	unchanged, err := s.packageUnchanged(ctx, blobs, group)
	if err != nil || !unchanged {
		return false, err
	}
	result.EvictedPackages++
	for _, digest := range group.Digests {
		if digest != "" {
			result.Candidates[digest] = true
		}
	}
	return true, nil
}

func packageFitsBudget(digests []string, limit int, result *PackageCleanupResult) bool {
	if limit == 0 || result.EvictedPackages == 0 {
		return true // Finish even one oversized package so it cannot starve forever.
	}
	count := len(result.Candidates)
	seen := make(map[string]bool, len(digests))
	for _, digest := range digests {
		if digest != "" && !result.Candidates[digest] && !seen[digest] {
			count++
			seen[digest] = true
		}
	}
	return count <= limit
}

func (s *Service) packageUnchanged(ctx context.Context, blobs map[string]meta.BlobRecord, group *ecosystem.CacheCleanupGroup) (bool, error) {
	tagsMatch, err := s.packageTagsUnchanged(ctx, group.Tags)
	if err != nil || !tagsMatch {
		return false, err
	}
	manifestsMatch, err := s.packageManifestsUnchanged(ctx, group.Manifests)
	if err != nil || !manifestsMatch {
		return false, err
	}
	return s.packageBlobsUnchanged(ctx, blobs, group.Digests)
}

func (s *Service) packageTagsUnchanged(ctx context.Context, tags []meta.TagRecord) (bool, error) {
	for i := range tags {
		expected := &tags[i]
		current, ok, err := s.metadata.Tag(ctx, meta.TagKey{Alias: expected.Alias, Repository: expected.Repository, Reference: expected.Reference})
		if err != nil {
			return false, fmt.Errorf("recheck package tag: %w", err)
		}
		if !ok || current.Digest != expected.Digest || !current.UpdatedAt.Equal(expected.UpdatedAt) {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) packageManifestsUnchanged(ctx context.Context, manifests []meta.ManifestRecord) (bool, error) {
	for i := range manifests {
		expected := &manifests[i]
		current, ok, err := s.metadata.Manifest(ctx, meta.ManifestKey{Alias: expected.Alias, Repository: expected.Repository, Digest: expected.Digest})
		if err != nil {
			return false, fmt.Errorf("recheck package manifest: %w", err)
		}
		if !ok || current.ObjectKey != expected.ObjectKey || !current.UpdatedAt.Equal(expected.UpdatedAt) {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) packageBlobsUnchanged(ctx context.Context, blobs map[string]meta.BlobRecord, digests []string) (bool, error) {
	for _, digest := range digests {
		unchanged, err := s.packageBlobUnchanged(ctx, blobs, digest)
		if err != nil || !unchanged {
			return false, err
		}
	}
	return true, nil
}

func (s *Service) packageBlobUnchanged(ctx context.Context, blobs map[string]meta.BlobRecord, digest string) (bool, error) {
	expected, present := blobs[digest]
	if !present {
		return s.packageObjectMissing(ctx, digest)
	}
	current, ok, err := s.metadata.Blob(ctx, meta.BlobKey{Digest: expected.Digest})
	if err != nil {
		return false, fmt.Errorf("recheck package access: %w", err)
	}
	return ok && sameBlobRevision(expected, *current), nil
}

func (s *Service) packageObjectMissing(ctx context.Context, digest string) (bool, error) {
	_, found, err := s.metadata.Blob(ctx, meta.BlobKey{Digest: digest})
	if err != nil {
		return false, fmt.Errorf("recheck missing package blob: %w", err)
	}
	if found {
		return false, nil
	}
	exists, err := s.objects.Exists(ctx, digest)
	if err != nil {
		return false, fmt.Errorf("recheck missing package object: %w", err)
	}
	return !exists, nil
}

func addOrphanCandidates(inventory ecosystem.CacheCleanupInventory, groups []ecosystem.CacheCleanupGroup, opts ecosystem.CacheCleanupOptions, result *PackageCleanupResult) {
	owned := make(map[string]bool)
	for i := range groups {
		group := &groups[i]
		for _, digest := range group.Digests {
			owned[digest] = true
		}
	}
	for i := range inventory.Blobs {
		blob := &inventory.Blobs[i]
		if owned[blob.Digest] || blob.LastAccessAt.IsZero() || !blob.LastAccessAt.Before(opts.Now.Add(-opts.UnusedFor)) {
			continue
		}
		if cleanupBudgetReached(opts, result) {
			result.LimitReached = true
			break
		}
		result.ScannedPackages++
		result.Candidates[blob.Digest] = true
	}
}

func cleanupBudgetReached(opts ecosystem.CacheCleanupOptions, result *PackageCleanupResult) bool {
	return (opts.MaxDeletes > 0 && len(result.Candidates) >= opts.MaxDeletes) ||
		(opts.MaxScan > 0 && result.ScannedPackages >= opts.MaxScan)
}
