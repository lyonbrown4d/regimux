package cachegc_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/cachegc"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/stretchr/testify/require"
)

func TestPackagePlanRetainsPackageAfterPullRecordedSinceInventory(t *testing.T) {
	tests := []struct {
		name        string
		ecosystem   string
		groupAlias  string
		pullAlias   string
		customGroup bool
	}{
		{name: "Go scoped alias", ecosystem: ecosystem.Go, pullAlias: ecosystem.ScopedAlias(ecosystem.Go, "mirror")},
		{name: "npm scoped alias", ecosystem: ecosystem.NPM, pullAlias: ecosystem.ScopedAlias(ecosystem.NPM, "mirror")},
		{name: "PyPI scoped alias", ecosystem: ecosystem.PyPI, pullAlias: ecosystem.ScopedAlias(ecosystem.PyPI, "mirror")},
		{name: "Maven scoped alias", ecosystem: ecosystem.Maven, pullAlias: ecosystem.ScopedAlias(ecosystem.Maven, "mirror")},
		{name: "dist raw alias", ecosystem: ecosystem.Dist, pullAlias: "mirror"},
		{name: "container raw alias", ecosystem: ecosystem.Container, pullAlias: "mirror", customGroup: true},
		{name: "already scoped alias", ecosystem: ecosystem.PyPI, groupAlias: "pypi/mirror", pullAlias: "pypi/mirror"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			metadata, objects := testStores(t)
			createdAt := time.Now().UTC()
			cache := artifactcache.New(artifactcache.Dependencies{
				Metadata: metadata, Objects: objects, Now: func() time.Time { return createdAt },
			})
			groupAlias := test.groupAlias
			if groupAlias == "" {
				groupAlias = "mirror"
			}
			key := artifactcache.Key{Alias: groupAlias, Repository: "packages/demo", Reference: "demo-1.0.whl"}
			entry, err := cache.Put(ctx, artifactcache.PutRequest{
				Key: key, AcceptKey: test.ecosystem, Body: strings.NewReader("package artifact"),
				ContentType: "application/octet-stream",
			})
			require.NoError(t, err)
			require.NoError(t, entry.Body.Close())

			inventory, err := cachegc.LoadCleanupInventory(ctx, metadata)
			require.NoError(t, err)
			require.Empty(t, inventory.Pulls)
			now := time.Now().UTC().Add(30 * 24 * time.Hour)
			opts := ecosystem.CacheCleanupOptions{Now: now, UnusedFor: 7 * 24 * time.Hour}
			var groups []ecosystem.CacheCleanupGroup
			if test.customGroup {
				groups = []ecosystem.CacheCleanupGroup{{
					Ecosystem: ecosystem.Container, Alias: key.Alias,
					Package:      key.Repository + "@" + entry.Digest,
					LastAccessAt: createdAt,
					Tags:         inventory.Tags, Manifests: inventory.Manifests, Digests: []string{entry.Digest},
				}}
			} else {
				groups, err = artifactcache.PlanCacheCleanup(ctx, test.ecosystem, inventory, opts, func(repository, _ string) string {
					return repository
				})
				require.NoError(t, err)
			}
			require.Len(t, groups, 1)
			require.True(t, groups[0].LastAccessAt.Before(opts.Now.Add(-opts.UnusedFor)), "fixture package must be stale before the new pull")

			pullKey := meta.PullKey{Alias: test.pullAlias, Repository: key.Repository, Reference: key.Reference}
			_, err = metadata.RecordPull(ctx, pullKey, opts.Now)
			require.NoError(t, err)
			pull, found, err := metadata.Pull(ctx, pullKey)
			require.NoError(t, err)
			require.True(t, found)
			require.WithinDuration(t, opts.Now, pull.LastPullAt, time.Second)

			plan, err := cachegc.NewCleanupService(metadata, objects).ApplyCleanupPlan(ctx, inventory, groups, opts)
			require.NoError(t, err)
			require.Zero(t, plan.EvictedPackages, "a post-inventory pull must invalidate the stale package decision")
			require.Empty(t, plan.Candidates)
			require.Zero(t, plan.RetiredReferences)
			assertPackageReferences(ctx, t, metadata, 1)
			assertExists(ctx, t, objects, entry.Digest)
		})
	}
}

func TestPackagePlanAbortsWhenPullRevalidationFails(t *testing.T) {
	metadata, objects, inventory, groups, opts := packagePlanFixture(t)
	pullErr := errors.New("synthetic pull lookup failure")
	failing := pullFailureMetadata{Store: metadata, err: pullErr}

	_, err := cachegc.NewCleanupService(failing, objects).ApplyCleanupPlan(t.Context(), inventory, groups, opts)
	require.ErrorIs(t, err, pullErr)
	assertPackageReferences(t.Context(), t, metadata, 2)
}

func TestPackagePlanDoesNotCacheMissingPullAcrossGroups(t *testing.T) {
	metadata, objects, inventory, groups, opts := packagePlanFixture(t)
	group := groups[0]
	group.Package += "-duplicate-owner"
	groups = append(groups, group)
	tag := groups[0].Tags[0]
	pullKey := meta.PullKey{
		Alias: ecosystem.ScopedAlias(ecosystem.Maven, tag.Alias), Repository: tag.Repository, Reference: tag.Reference,
	}
	appearing := pullAppearsAfterMissMetadata{Store: metadata, key: pullKey, at: opts.Now}

	plan, err := cachegc.NewCleanupService(&appearing, objects).ApplyCleanupPlan(t.Context(), inventory, groups, opts)
	require.NoError(t, err)
	require.Equal(t, 1, plan.EvictedPackages, "the second group must observe the Pull added after the first miss")
	assertPackageReferences(t.Context(), t, metadata, 2)
}

type pullAppearsAfterMissMetadata struct {
	meta.Store
	key      meta.PullKey
	at       time.Time
	inserted bool
}

func (m *pullAppearsAfterMissMetadata) Pull(ctx context.Context, key meta.PullKey) (*meta.PullRecord, bool, error) {
	pull, found, err := m.Store.Pull(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("read test pull: %w", err)
	}
	if found || key != m.key || m.inserted {
		return pull, found, nil
	}
	m.inserted = true
	if _, err := m.RecordPull(ctx, key, m.at); err != nil {
		return nil, false, fmt.Errorf("record test pull after miss: %w", err)
	}
	return nil, false, nil
}

func TestPackagePlanFindsDigestPullForUntaggedManifest(t *testing.T) {
	ctx := t.Context()
	metadata, objects := testStores(t)
	createdAt := time.Now().UTC()
	cache := artifactcache.New(artifactcache.Dependencies{
		Metadata: metadata, Objects: objects, Now: func() time.Time { return createdAt },
	})
	key := artifactcache.Key{Alias: "mirror", Repository: "packages/demo", Reference: "demo-1.0.whl"}
	entry, err := cache.Put(ctx, artifactcache.PutRequest{
		Key: key, AcceptKey: ecosystem.PyPI, Body: strings.NewReader("package artifact"),
		ContentType: "application/octet-stream",
	})
	require.NoError(t, err)
	require.NoError(t, entry.Body.Close())
	require.NoError(t, metadata.DeleteTag(ctx, meta.TagKey{Alias: key.Alias, Repository: key.Repository, Reference: key.Reference}))

	inventory, err := cachegc.LoadCleanupInventory(ctx, metadata)
	require.NoError(t, err)
	require.Empty(t, inventory.Tags)
	manifest := inventory.Manifests[0]
	manifest.Reference = ""
	inventory.Manifests = []meta.ManifestRecord{manifest}
	now := time.Now().UTC().Add(30 * 24 * time.Hour)
	opts := ecosystem.CacheCleanupOptions{Now: now, UnusedFor: 7 * 24 * time.Hour}
	groups := []ecosystem.CacheCleanupGroup{{
		Ecosystem: ecosystem.PyPI, Alias: key.Alias, Package: key.Repository + "@" + entry.Digest,
		LastAccessAt: createdAt, Manifests: []meta.ManifestRecord{manifest}, Digests: []string{entry.Digest},
	}}
	_, err = metadata.RecordPull(ctx, meta.PullKey{Alias: ecosystem.ScopedAlias(ecosystem.PyPI, key.Alias), Repository: key.Repository, Reference: entry.Digest}, now)
	require.NoError(t, err)

	plan, err := cachegc.NewCleanupService(metadata, objects).ApplyCleanupPlan(ctx, inventory, groups, opts)
	require.NoError(t, err)
	require.Zero(t, plan.EvictedPackages)
	require.Empty(t, plan.Candidates)
	assertExists(ctx, t, objects, entry.Digest)
}

type pullFailureMetadata struct {
	meta.Store
	err error
}

func (m pullFailureMetadata) Pull(context.Context, meta.PullKey) (*meta.PullRecord, bool, error) {
	return nil, false, m.err
}
