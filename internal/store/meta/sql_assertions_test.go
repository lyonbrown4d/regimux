package meta_test

import (
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func assertPullRecord(t *testing.T, pull *meta.PullRecord, count int64, lastPullAt, lastUpstreamPullAt time.Time) {
	t.Helper()

	if pull.Key != "hub/library/node:20" || pull.Count != count || !pull.LastPullAt.Equal(lastPullAt) {
		t.Fatalf("unexpected pull record: %#v", pull)
	}
	if !lastUpstreamPullAt.IsZero() && !pull.LastUpstreamPullAt.Equal(lastUpstreamPullAt) {
		t.Fatalf("unexpected upstream pull record: %#v", pull)
	}
	if count > 1 && pull.CreatedAt.IsZero() {
		t.Fatalf("unexpected pull created_at: %#v", pull)
	}
}

func assertPullLookup(t *testing.T, got *meta.PullRecord, ok bool, lastUpstreamPullAt time.Time) {
	t.Helper()

	if !ok || got.Count != 2 || !got.LastUpstreamPullAt.Equal(lastUpstreamPullAt) {
		t.Fatalf("unexpected pull lookup: ok=%v record=%#v", ok, got)
	}
}

func assertRepositoryAggregate(
	t *testing.T,
	repository *meta.Repository,
	pullCount int64,
	blobBytes int64,
	blobLinkCount int64,
	lastPullAt time.Time,
	lastBlobAccessAt time.Time,
) {
	t.Helper()

	if repository.PullCount != pullCount || repository.BlobBytes != blobBytes || repository.BlobLinkCount != blobLinkCount {
		t.Fatalf("unexpected repository aggregate counters: %#v", repository)
	}
	if !repository.LastPullAt.Equal(lastPullAt) || !repository.LastBlobAccessAt.Equal(lastBlobAccessAt) {
		t.Fatalf("unexpected repository aggregate times: %#v", repository)
	}
	if repository.LastActivityAt.IsZero() {
		t.Fatalf("expected repository last activity: %#v", repository)
	}
}

func seedListRecords(ctx context.Context, t *testing.T, store *meta.SQLStore, expires time.Time) {
	t.Helper()

	upsertManifest(ctx, t, store, expires)
	_, err := store.UpsertTag(ctx, meta.TagRecord{
		Alias:      "hub",
		Repository: "library/nginx",
		Reference:  "latest",
		Digest:     testDigest,
		ExpiresAt:  expires,
	})
	requireNoError(t, "upsert tag", err)
	_, err = store.UpsertBlob(ctx, meta.BlobRecord{
		Digest:       secondTestDigest,
		Size:         42,
		LastAccessAt: time.Now().UTC(),
	})
	requireNoError(t, "upsert blob", err)
	_, err = store.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:      "hub",
		Repository: "library/nginx",
		Digest:     secondTestDigest,
	})
	requireNoError(t, "upsert repo blob", err)
	_, err = store.RecordPull(ctx, meta.PullKey{Alias: "hub", Repository: "library/node", Reference: "20"}, time.Now().UTC())
	requireNoError(t, "record pull", err)
}

func assertManifestList(t *testing.T, manifests []meta.ManifestRecord) {
	t.Helper()

	if len(manifests) != 1 || manifests[0].Digest != testDigest {
		t.Fatalf("unexpected manifests: %#v", manifests)
	}
}

func assertTagList(t *testing.T, tags []meta.TagRecord) {
	t.Helper()

	if len(tags) != 1 || tags[0].Reference != "latest" {
		t.Fatalf("unexpected tags: %#v", tags)
	}
}

func assertPullList(t *testing.T, pulls []meta.PullRecord) {
	t.Helper()

	if len(pulls) != 1 || pulls[0].Reference != "20" {
		t.Fatalf("unexpected pulls: %#v", pulls)
	}
}

func assertBlobList(t *testing.T, blobs []meta.BlobRecord) {
	t.Helper()

	if len(blobs) != 1 || blobs[0].Digest != secondTestDigest {
		t.Fatalf("unexpected blobs: %#v", blobs)
	}
}

func assertRepoBlobList(t *testing.T, repoBlobs []meta.RepoBlobRecord) {
	t.Helper()

	if len(repoBlobs) != 1 || repoBlobs[0].Digest != secondTestDigest || repoBlobs[0].LastAccessAt.IsZero() {
		t.Fatalf("unexpected repo blobs: %#v", repoBlobs)
	}
}

func seedStatsRecords(ctx context.Context, t *testing.T, store *meta.SQLStore, now time.Time) {
	t.Helper()

	upsertStatsManifest(ctx, t, store, "library/node", testDigest, 100, now.Add(-time.Hour))
	upsertStatsManifest(ctx, t, store, "library/redis", secondTestDigest, 200, now.Add(time.Hour))
	upsertStatsTag(ctx, t, store, "library/node", "18", testDigest, now.Add(-time.Hour))
	upsertStatsTag(ctx, t, store, "library/redis", "7", secondTestDigest, now.Add(time.Hour))
	upsertStatsBlob(ctx, t, store, testDigest, 100, now.Add(-3*time.Hour))
	upsertStatsBlob(ctx, t, store, secondTestDigest, 500, now.Add(-time.Hour))
	upsertStatsBlob(ctx, t, store, thirdTestDigest, 300, now.Add(-2*time.Hour))
	upsertStatsRepoBlob(ctx, t, store, testDigest, now.Add(-3*time.Hour))
	upsertStatsRepoBlob(ctx, t, store, secondTestDigest, now.Add(-time.Hour))
	upsertStatsRepoBlob(ctx, t, store, thirdTestDigest, now.Add(-2*time.Hour))
	recordStatsPull(ctx, t, store, "18", now.Add(-3*time.Hour))
	recordStatsPull(ctx, t, store, "20", now.Add(-time.Hour))
	recordStatsPull(ctx, t, store, "19", now.Add(-2*time.Hour))
	_, err := store.RecordUpstreamPull(ctx, meta.PullKey{
		Alias:      "hub",
		Repository: "library/node",
		Reference:  "19",
	}, now.Add(-30*time.Minute))
	requireNoError(t, "record upstream pull", err)
}

func upsertStatsManifest(
	ctx context.Context,
	t *testing.T,
	store *meta.SQLStore,
	repository string,
	digest string,
	size int64,
	expiresAt time.Time,
) {
	t.Helper()

	_, err := store.UpsertManifest(ctx, meta.ManifestRecord{
		Alias:      "hub",
		Repository: repository,
		Digest:     digest,
		MediaType:  distribution.MediaTypeOCIManifest,
		Size:       size,
		ExpiresAt:  expiresAt,
	})
	requireNoError(t, "upsert stats manifest", err)
}

func upsertStatsTag(
	ctx context.Context,
	t *testing.T,
	store *meta.SQLStore,
	repository string,
	reference string,
	digest string,
	expiresAt time.Time,
) {
	t.Helper()

	_, err := store.UpsertTag(ctx, meta.TagRecord{
		Alias:      "hub",
		Repository: repository,
		Reference:  reference,
		Digest:     digest,
		ExpiresAt:  expiresAt,
	})
	requireNoError(t, "upsert stats tag", err)
}

func upsertStatsBlob(ctx context.Context, t *testing.T, store *meta.SQLStore, digest string, size int64, at time.Time) {
	t.Helper()

	_, err := store.UpsertBlob(ctx, meta.BlobRecord{
		Digest:       digest,
		Size:         size,
		MediaType:    distribution.MediaTypeOctetStream,
		LastAccessAt: at,
		UpdatedAt:    at,
	})
	requireNoError(t, "upsert stats blob", err)
}

func upsertStatsRepoBlob(ctx context.Context, t *testing.T, store *meta.SQLStore, digest string, at time.Time) {
	t.Helper()

	_, err := store.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:          "hub",
		Repository:     "library/node",
		Digest:         digest,
		SourceManifest: fourthTestDigest,
		LastAccessAt:   at,
		LastVerifiedAt: at,
	})
	requireNoError(t, "upsert stats repo blob", err)
}

func recordStatsPull(ctx context.Context, t *testing.T, store *meta.SQLStore, reference string, at time.Time) {
	t.Helper()

	_, err := store.RecordPull(ctx, meta.PullKey{
		Alias:      "hub",
		Repository: "library/node",
		Reference:  reference,
	}, at)
	requireNoError(t, "record stats pull", err)
}

func assertMetadataStats(t *testing.T, stats meta.MetadataStats, now time.Time) {
	t.Helper()
	assertManifestStats(t, stats)
	assertTagStats(t, stats)
	assertBlobStats(t, stats)
	assertPullStats(t, stats, now)
}

func assertManifestStats(t *testing.T, stats meta.MetadataStats) {
	t.Helper()
	if stats.ManifestCount != 2 || stats.ExpiredManifestCount != 1 || stats.ManifestBytes != 300 {
		t.Fatalf("unexpected manifest stats: %#v", stats)
	}
}

func assertTagStats(t *testing.T, stats meta.MetadataStats) {
	t.Helper()
	if stats.TagCount != 2 || stats.ExpiredTagCount != 1 {
		t.Fatalf("unexpected tag stats: %#v", stats)
	}
}

func assertBlobStats(t *testing.T, stats meta.MetadataStats) {
	t.Helper()
	if stats.BlobCount != 3 || stats.BlobBytes != 900 || stats.RepoBlobCount != 3 {
		t.Fatalf("unexpected blob stats: %#v", stats)
	}
}

func assertPullStats(t *testing.T, stats meta.MetadataStats, now time.Time) {
	t.Helper()
	if stats.PullCount != 3 || !stats.LastPullAt.Equal(now.Add(-time.Hour)) {
		t.Fatalf("unexpected pull stats: %#v", stats)
	}
	if !stats.LastUpstreamPullAt.Equal(now.Add(-30 * time.Minute)) {
		t.Fatalf("unexpected upstream pull stats: %#v", stats)
	}
}
