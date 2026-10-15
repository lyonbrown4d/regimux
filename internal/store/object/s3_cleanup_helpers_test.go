package object_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"github.com/lyonbrown4d/regimux/internal/cachegc"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func applyS3MavenCleanup(ctx context.Context, t *testing.T, service *cachegc.Service, metadata meta.Store, objects object.Store, repository string, now time.Time, dryRun bool) (*cachegc.PackageCleanupResult, *cachegc.CleanupReport) {
	t.Helper()
	inventory, err := cachegc.LoadCleanupInventory(ctx, metadata)
	require.NoError(t, err)
	group := s3MavenGroup(inventory, repository, now.Add(-30*24*time.Hour))
	plan, err := service.ApplyCleanupPlan(ctx, inventory, []ecosystem.CacheCleanupGroup{group}, ecosystem.CacheCleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour, DryRun: dryRun,
	})
	require.NoError(t, err)
	report, err := cachegc.NewCleanupService(plan.Metadata, objects).CleanupBlobs(ctx, cachegc.CleanupOptions{
		Now: now, UnusedFor: 7 * 24 * time.Hour, DryRun: dryRun, Candidates: plan.Candidates,
	})
	require.NoError(t, err)
	return plan, report
}

func seedS3MavenArtifacts(ctx context.Context, t *testing.T, store object.Store, metadata meta.Store, repository string, artifacts map[string]string, at time.Time) []string {
	t.Helper()
	digests := make([]string, 0, len(artifacts))
	for reference, body := range artifacts {
		digestValue := seedS3CleanupBlob(ctx, t, store, metadata, []byte(body), at)
		seedS3MavenReference(ctx, t, metadata, repository, reference, digestValue, at)
		digests = append(digests, digestValue)
	}
	return digests
}

func seedS3MavenReference(ctx context.Context, t *testing.T, metadata meta.Store, repository, reference, digestValue string, at time.Time) {
	t.Helper()
	_, err := metadata.UpsertManifest(ctx, meta.ManifestRecord{
		Alias: "maven", Repository: repository, Reference: reference, Digest: digestValue,
		MediaType: "application/octet-stream", ObjectKey: digestValue, CreatedAt: at, UpdatedAt: at,
	})
	require.NoError(t, err)
	_, err = metadata.UpsertTag(ctx, meta.TagRecord{
		Alias: "maven", Repository: repository, Reference: reference, Digest: digestValue, CreatedAt: at, UpdatedAt: at,
	})
	require.NoError(t, err)
	_, err = metadata.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias: "maven", Repository: repository, Digest: digestValue, CreatedAt: at, UpdatedAt: at, LastAccessAt: at,
	})
	require.NoError(t, err)
}

func s3MavenGroup(inventory ecosystem.CacheCleanupInventory, repository string, lastAccess time.Time) ecosystem.CacheCleanupGroup {
	group := ecosystem.CacheCleanupGroup{Ecosystem: "maven", Alias: "maven", Package: repository, LastAccessAt: lastAccess}
	appendS3MavenManifests(&group, inventory.Manifests, repository)
	appendS3MavenTags(&group, inventory.Tags, repository)
	appendS3MavenRepoBlobs(&group, inventory.RepoBlobs, repository)
	return group
}

func appendS3MavenManifests(group *ecosystem.CacheCleanupGroup, records []meta.ManifestRecord, repository string) {
	for index := range records {
		manifest := records[index]
		if manifest.Alias != group.Alias || manifest.Repository != repository {
			continue
		}
		group.Manifests = append(group.Manifests, manifest)
		group.Digests = append(group.Digests, manifest.Digest)
	}
}

func appendS3MavenTags(group *ecosystem.CacheCleanupGroup, records []meta.TagRecord, repository string) {
	for index := range records {
		tag := records[index]
		if tag.Alias == group.Alias && tag.Repository == repository {
			group.Tags = append(group.Tags, tag)
		}
	}
}

func appendS3MavenRepoBlobs(group *ecosystem.CacheCleanupGroup, records []meta.RepoBlobRecord, repository string) {
	for index := range records {
		link := records[index]
		if link.Alias == group.Alias && link.Repository == repository {
			group.RepoBlobs = append(group.RepoBlobs, link)
		}
	}
}

func newS3CleanupMetadata(ctx context.Context, t *testing.T) meta.Store {
	t.Helper()
	metadata, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "cleanup.db")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, metadata.Close()) })
	return metadata
}

func seedS3CleanupArtifact(ctx context.Context, t *testing.T, store object.Store, metadata meta.Store, alias string, body []byte, mediaType string, at time.Time) string {
	t.Helper()
	digestValue := seedS3CleanupBlob(ctx, t, store, metadata, body, at)
	repository := alias + "-repository"
	seedS3CleanupReference(ctx, t, metadata, alias, repository, "v1", digestValue, mediaType, at)
	_, err := metadata.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias: alias, Repository: repository, Digest: digestValue,
		CreatedAt: at, UpdatedAt: at, LastAccessAt: at,
	})
	require.NoError(t, err)
	return digestValue
}

func seedS3CleanupBlob(ctx context.Context, t *testing.T, store object.Store, metadata meta.Store, body []byte, at time.Time) string {
	t.Helper()
	digestValue := digest.SHA256.FromBytes(body).String()
	info, err := store.Put(ctx, digestValue, bytes.NewReader(body), object.PutOptions{ContentType: "application/octet-stream"})
	require.NoError(t, err)
	_, err = metadata.UpsertBlob(ctx, meta.BlobRecord{
		Digest: digestValue, Size: info.Size, MediaType: "application/octet-stream", ObjectKey: digestValue,
		CreatedAt: at, UpdatedAt: at, LastAccessAt: at,
	})
	require.NoError(t, err)
	return digestValue
}

func seedS3CleanupReference(ctx context.Context, t *testing.T, metadata meta.Store, alias, repository, reference, digestValue, mediaType string, at time.Time) {
	t.Helper()
	_, err := metadata.UpsertManifest(ctx, meta.ManifestRecord{
		Alias: alias, Repository: repository, Reference: reference, Digest: digestValue,
		MediaType: mediaType, ObjectKey: digestValue, CreatedAt: at, UpdatedAt: at,
	})
	require.NoError(t, err)
	_, err = metadata.UpsertTag(ctx, meta.TagRecord{
		Alias: alias, Repository: repository, Reference: reference, Digest: digestValue,
		CreatedAt: at, UpdatedAt: at,
	})
	require.NoError(t, err)
}

func assertS3CleanupRefPresent(ctx context.Context, t *testing.T, metadata meta.Store, alias, repository, reference string) {
	t.Helper()
	_, ok, err := metadata.Tag(ctx, meta.TagKey{Alias: alias, Repository: repository, Reference: reference})
	require.NoError(t, err)
	require.True(t, ok)
}

func assertS3CleanupRefAbsent(ctx context.Context, t *testing.T, metadata meta.Store, alias, repository, reference string) {
	t.Helper()
	_, ok, err := metadata.Tag(ctx, meta.TagKey{Alias: alias, Repository: repository, Reference: reference})
	require.NoError(t, err)
	if ok {
		tags, listErr := metadata.ListTags(ctx)
		require.NoError(t, listErr)
		tags.Range(func(_ int, tag meta.TagRecord) bool {
			t.Logf("remaining tag: %s/%s:%s digest=%s", tag.Alias, tag.Repository, tag.Reference, tag.Digest)
			return true
		})
	}
	require.False(t, ok, "stale tag %s/%s:%s should be pruned", alias, repository, reference)
}

func s3FakeKeyExists(client *fakeS3Client, key string) bool {
	_, ok := client.object(key)
	return ok
}

type s3CleanupDeleteClient struct {
	*fakeS3Client
	failDeletes bool
}

func (c *s3CleanupDeleteClient) DeleteObject(ctx context.Context, input *s3.DeleteObjectInput, options ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if c.failDeletes {
		return nil, errors.New("injected DeleteObject failure")
	}
	return c.fakeS3Client.DeleteObject(ctx, input, options...)
}

type s3CleanupDeleteTagMetadata struct {
	meta.Store
	failDeleteTag bool
}

func (m *s3CleanupDeleteTagMetadata) DeleteTag(ctx context.Context, key meta.TagKey) error {
	if m.failDeleteTag {
		m.failDeleteTag = false
		return errors.New("injected DeleteTag failure")
	}
	if err := m.Store.DeleteTag(ctx, key); err != nil {
		return fmt.Errorf("delete cleanup test tag: %w", err)
	}
	return nil
}

var _ object.S3Client = (*s3CleanupDeleteClient)(nil)
var _ meta.Store = (*s3CleanupDeleteTagMetadata)(nil)
