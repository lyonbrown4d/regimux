package object_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lyonbrown4d/regimux/internal/cachegc"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestS3SharedCleanupDeletesIdleArtifactsAcrossEcosystems(t *testing.T) {
	ctx := t.Context()
	fixture := newS3Fixture(t)
	metadata := newS3CleanupMetadata(ctx, t)
	now := time.Now().UTC().Add(14 * 24 * time.Hour)
	stale := now.Add(-14 * 24 * time.Hour)

	aliases := []string{"go", "npm", "pypi", "maven", "dist", "hub"}
	digests := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		body := []byte(alias + " artifact")
		mediaType := "application/octet-stream"
		if alias == "hub" {
			body = []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":0},"layers":[]}`)
			mediaType = "application/vnd.oci.image.manifest.v1+json"
		}
		digests = append(digests, seedS3CleanupArtifact(ctx, t, fixture.store, metadata, alias, body, mediaType, stale))
	}
	shared := seedS3CleanupBlob(ctx, t, fixture.store, metadata, []byte("shared artifact"), stale)
	seedS3CleanupReference(ctx, t, metadata, "go", "shared-go", "old", shared, "application/octet-stream", stale)
	seedS3CleanupReference(ctx, t, metadata, "npm", "shared", "current", shared, "application/octet-stream", now)
	_, err := metadata.RecordPull(ctx, meta.PullKey{Alias: "npm", Repository: "shared", Reference: "current"}, now)
	require.NoError(t, err)
	fresh := seedS3CleanupBlob(ctx, t, fixture.store, metadata, []byte("fresh artifact"), now)

	otherPrefix, err := object.NewS3WithClient(fixture.client, object.S3Options{Bucket: "bucket", Prefix: "tenant/other"})
	require.NoError(t, err)
	_, err = otherPrefix.Put(ctx, digests[0], strings.NewReader("go artifact"), object.PutOptions{})
	require.NoError(t, err)
	otherKey := testS3Key("tenant/other", digests[0])
	require.True(t, s3FakeKeyExists(fixture.client, otherKey))

	report, err := cachegc.NewCleanupService(metadata, fixture.store).CleanupBlobs(ctx, cachegc.CleanupOptions{
		UnusedFor: 7 * 24 * time.Hour, MaxDeletes: 100, Now: now, PruneReferences: true,
	})
	require.NoError(t, err)
	require.Equal(t, len(aliases), report.DeletedBlobs)
	for _, value := range digests {
		require.False(t, s3FakeKeyExists(fixture.client, testS3Key("tenant/cache", value)), "stale %s artifact should be deleted", value)
	}
	require.True(t, s3FakeKeyExists(fixture.client, testS3Key("tenant/cache", shared)), "fresh shared reference should protect its blob")
	require.True(t, s3FakeKeyExists(fixture.client, testS3Key("tenant/cache", fresh)), "recently used blob should remain")
	require.True(t, s3FakeKeyExists(fixture.client, otherKey), "cleanup must leave objects under another configured prefix")

	for _, alias := range aliases {
		assertS3CleanupRefAbsent(ctx, t, metadata, alias, alias+"-repository", "v1")
	}
	assertS3CleanupRefPresent(ctx, t, metadata, "npm", "shared", "current")
	assertS3CleanupRefPresent(ctx, t, metadata, "go", "shared-go", "old")
}

func TestS3SharedCleanupDryRunDoesNotDeleteObjectsOrMetadata(t *testing.T) {
	ctx := t.Context()
	fixture := newS3Fixture(t)
	metadata := newS3CleanupMetadata(ctx, t)
	now := time.Now().UTC().Add(14 * 24 * time.Hour)
	digestValue := seedS3CleanupArtifact(ctx, t, fixture.store, metadata, "npm", []byte("dry run"), "application/octet-stream", now.Add(-14*24*time.Hour))

	report, err := cachegc.NewCleanupService(metadata, fixture.store).CleanupBlobs(ctx, cachegc.CleanupOptions{
		UnusedFor: 7 * 24 * time.Hour, DryRun: true, Now: now, PruneReferences: true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, report.DeletedBlobs)
	require.True(t, s3FakeKeyExists(fixture.client, testS3Key("tenant/cache", digestValue)))
	_, found, err := metadata.Blob(ctx, meta.BlobKey{Digest: digestValue})
	require.NoError(t, err)
	require.True(t, found, "dry-run must preserve retry metadata")
	assertS3CleanupRefPresent(ctx, t, metadata, "npm", "npm-repository", "v1")
}

func TestS3SharedCleanupDeleteErrorPreservesMetadataForRetry(t *testing.T) {
	ctx := t.Context()
	fake := newFakeS3Client()
	client := &s3CleanupDeleteClient{fakeS3Client: fake, failDeletes: true}
	store, err := object.NewS3WithClient(client, object.S3Options{Bucket: "bucket", Prefix: "tenant/cache"})
	require.NoError(t, err)
	metadata := newS3CleanupMetadata(ctx, t)
	now := time.Now().UTC().Add(14 * 24 * time.Hour)
	digestValue := seedS3CleanupArtifact(ctx, t, store, metadata, "maven", []byte("retry"), "application/octet-stream", now.Add(-14*24*time.Hour))
	service := cachegc.NewCleanupService(metadata, store)
	options := cachegc.CleanupOptions{UnusedFor: 7 * 24 * time.Hour, Now: now, PruneReferences: true}

	_, err = service.CleanupBlobs(ctx, options)
	require.Error(t, err)
	require.True(t, s3FakeKeyExists(fake, testS3Key("tenant/cache", digestValue)))
	_, found, metaErr := metadata.Blob(ctx, meta.BlobKey{Digest: digestValue})
	require.NoError(t, metaErr)
	require.True(t, found, "failed deletion must preserve metadata for the next cleanup attempt")
	assertS3CleanupRefPresent(ctx, t, metadata, "maven", "maven-repository", "v1")

	client.failDeletes = false
	_, err = service.CleanupBlobs(ctx, options)
	require.NoError(t, err)
	require.False(t, s3FakeKeyExists(fake, testS3Key("tenant/cache", digestValue)))
	_, found, metaErr = metadata.Blob(ctx, meta.BlobKey{Digest: digestValue})
	require.NoError(t, metaErr)
	require.False(t, found)
	assertS3CleanupRefAbsent(ctx, t, metadata, "maven", "maven-repository", "v1")
}

func TestS3SharedCleanupReferenceDeleteErrorPreservesBlobForRetry(t *testing.T) {
	ctx := t.Context()
	fixture := newS3Fixture(t)
	baseMetadata := newS3CleanupMetadata(ctx, t)
	metadata := &s3CleanupDeleteTagMetadata{Store: baseMetadata, failDeleteTag: true}
	now := time.Now().UTC().Add(14 * 24 * time.Hour)
	digestValue := seedS3CleanupArtifact(ctx, t, fixture.store, metadata, "go", []byte("retry references"), "application/octet-stream", now.Add(-14*24*time.Hour))
	service := cachegc.NewCleanupService(metadata, fixture.store)
	options := cachegc.CleanupOptions{UnusedFor: 7 * 24 * time.Hour, Now: now, PruneReferences: true}

	_, err := service.CleanupBlobs(ctx, options)
	require.Error(t, err)
	require.False(t, s3FakeKeyExists(fixture.client, testS3Key("tenant/cache", digestValue)), "the object delete precedes reference pruning")
	_, found, metaErr := baseMetadata.Blob(ctx, meta.BlobKey{Digest: digestValue})
	require.NoError(t, metaErr)
	require.True(t, found, "keep the blob row as a retry marker when reference pruning fails")
	assertS3CleanupRefPresent(ctx, t, baseMetadata, "go", "go-repository", "v1")

	_, err = service.CleanupBlobs(ctx, options)
	require.NoError(t, err)
	_, found, metaErr = baseMetadata.Blob(ctx, meta.BlobKey{Digest: digestValue})
	require.NoError(t, metaErr)
	require.False(t, found)
	assertS3CleanupRefAbsent(ctx, t, baseMetadata, "go", "go-repository", "v1")
}

func TestS3PackageCleanupFinishesOversizedMavenGAVBeforeNextGroup(t *testing.T) {
	ctx := t.Context()
	fixture := newS3Fixture(t)
	metadata := newS3CleanupMetadata(ctx, t)
	now := time.Now().UTC().Add(30 * 24 * time.Hour)
	stale := now.Add(-30 * 24 * time.Hour)
	firstGAV := "org/acme/demo/1.2.3"
	secondGAV := "org/acme/demo/2.0.0"
	firstArtifacts := map[string]string{
		"demo-1.2.3.jar":        "jar bytes",
		"demo-1.2.3.pom":        "pom bytes",
		"demo-1.2.3.pom.sha256": "checksum bytes",
		"demo-1.2.3.pom.asc":    "signature bytes",
	}
	firstDigests := seedS3MavenArtifacts(ctx, t, fixture.store, metadata, firstGAV, firstArtifacts, stale)
	secondDigests := seedS3MavenArtifacts(ctx, t, fixture.store, metadata, secondGAV, map[string]string{
		"demo-2.0.0.jar": "second version jar",
	}, stale)

	inventory, err := cachegc.LoadCleanupInventory(ctx, metadata)
	require.NoError(t, err)
	groups := []ecosystem.CacheCleanupGroup{
		s3MavenGroup(inventory, firstGAV, stale),
		s3MavenGroup(inventory, secondGAV, stale),
	}
	service := cachegc.NewCleanupService(metadata, fixture.store)
	plan, err := service.ApplyCleanupPlan(ctx, inventory, groups, ecosystem.CacheCleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour, MaxDeletes: 1,
	})
	require.NoError(t, err)
	require.Equal(t, 1, plan.EvictedPackages)
	require.Len(t, plan.Candidates, len(firstArtifacts), "the first GAV completes even when it exceeds the digest limit")
	for _, digestValue := range firstDigests {
		require.True(t, plan.Candidates[digestValue])
	}
	for _, digestValue := range secondDigests {
		require.False(t, plan.Candidates[digestValue], "the later GAV stays whole for the next pass")
	}

	report, err := cachegc.NewCleanupService(plan.Metadata, fixture.store).CleanupBlobs(ctx, cachegc.CleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour, Candidates: plan.Candidates,
	})
	require.NoError(t, err)
	require.Equal(t, len(firstArtifacts), report.DeletedBlobs)
	for _, digestValue := range firstDigests {
		require.False(t, s3FakeKeyExists(fixture.client, testS3Key("tenant/cache", digestValue)))
	}
	for _, digestValue := range secondDigests {
		require.True(t, s3FakeKeyExists(fixture.client, testS3Key("tenant/cache", digestValue)))
	}
}

func TestS3PackageCleanupDryRunMatchesRealMavenGroupRemoval(t *testing.T) {
	ctx := t.Context()
	fixture := newS3Fixture(t)
	metadata := newS3CleanupMetadata(ctx, t)
	now := time.Now().UTC().Add(30 * 24 * time.Hour)
	stale := now.Add(-30 * 24 * time.Hour)
	artifacts := map[string]string{
		"demo-1.2.3.jar":        "jar bytes",
		"demo-1.2.3.pom":        "pom bytes",
		"demo-1.2.3.pom.sha256": "checksum bytes",
	}
	digests := seedS3MavenArtifacts(ctx, t, fixture.store, metadata, "org/acme/demo/1.2.3", artifacts, stale)

	service := cachegc.NewCleanupService(metadata, fixture.store)
	dryPlan, dryReport := applyS3MavenCleanup(ctx, t, service, metadata, fixture.store, "org/acme/demo/1.2.3", now, true)
	require.Equal(t, 1, dryPlan.EvictedPackages)
	require.Equal(t, len(artifacts), dryReport.DeletedBlobs)
	for _, digestValue := range digests {
		require.True(t, s3FakeKeyExists(fixture.client, testS3Key("tenant/cache", digestValue)), "dry-run must leave object bytes unchanged")
	}
	require.ElementsMatch(t, digests, dryReport.DeletedDigests)
	loaded, err := cachegc.LoadCleanupInventory(ctx, metadata)
	require.NoError(t, err)
	require.Len(t, loaded.Manifests, len(artifacts), "dry-run must leave package metadata unchanged")

	realPlan, realReport := applyS3MavenCleanup(ctx, t, service, metadata, fixture.store, "org/acme/demo/1.2.3", now, false)
	require.Equal(t, dryPlan.Candidates, realPlan.Candidates)
	require.Equal(t, dryReport.DeletedBlobs, realReport.DeletedBlobs)
	require.ElementsMatch(t, dryReport.DeletedDigests, realReport.DeletedDigests)
	for _, digestValue := range digests {
		require.False(t, s3FakeKeyExists(fixture.client, testS3Key("tenant/cache", digestValue)))
	}
}

func TestS3PackageCleanupRetainsSharedDigestReferencedByAnotherGAV(t *testing.T) {
	ctx := t.Context()
	fixture := newS3Fixture(t)
	metadata := newS3CleanupMetadata(ctx, t)
	now := time.Now().UTC().Add(30 * 24 * time.Hour)
	stale := now.Add(-30 * 24 * time.Hour)
	sharedDigest := seedS3CleanupBlob(ctx, t, fixture.store, metadata, []byte("shared pom bytes"), stale)
	oldGAV := "org/acme/demo/1.2.3"
	keptGAV := "org/acme/demo/2.0.0"
	seedS3MavenReference(ctx, t, metadata, oldGAV, "demo-1.2.3.pom", sharedDigest, stale)
	seedS3MavenReference(ctx, t, metadata, keptGAV, "demo-2.0.0.pom", sharedDigest, now)
	inventory, err := cachegc.LoadCleanupInventory(ctx, metadata)
	require.NoError(t, err)
	oldGroup := s3MavenGroup(inventory, oldGAV, stale)
	keptGroup := s3MavenGroup(inventory, keptGAV, now)
	service := cachegc.NewCleanupService(metadata, fixture.store)
	plan, err := service.ApplyCleanupPlan(ctx, inventory, []ecosystem.CacheCleanupGroup{oldGroup, keptGroup}, ecosystem.CacheCleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour,
	})
	require.NoError(t, err)
	require.Equal(t, 1, plan.EvictedPackages)
	require.True(t, plan.Candidates[sharedDigest])

	report, err := cachegc.NewCleanupService(plan.Metadata, fixture.store).CleanupBlobs(ctx, cachegc.CleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour, Candidates: plan.Candidates,
	})
	require.NoError(t, err)
	require.Zero(t, report.DeletedBlobs)
	require.True(t, s3FakeKeyExists(fixture.client, testS3Key("tenant/cache", sharedDigest)), "the retained GAV reference protects shared content")
	assertS3CleanupRefAbsent(ctx, t, metadata, "maven", oldGAV, "demo-1.2.3.pom")
	assertS3CleanupRefPresent(ctx, t, metadata, "maven", keptGAV, "demo-2.0.0.pom")
}
