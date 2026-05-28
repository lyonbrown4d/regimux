package meta_test

import (
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
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

func seedListRecords(ctx context.Context, t *testing.T, store *meta.SQLiteStore, expires time.Time) {
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
