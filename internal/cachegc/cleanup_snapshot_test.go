package cachegc_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cachegc"
	"github.com/lyonbrown4d/regimux/internal/jsonbudget"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/opencontainers/go-digest"
)

func TestCleanupSnapshotBudgetFailsClosedBeforeDeletion(t *testing.T) {
	baseCtx := context.Background()
	metadata, objects := testStores(t)
	now := time.Now().UTC()
	digests := []string{
		putBlob(baseCtx, t, metadata, objects, []byte("old one"), now.Add(-48*time.Hour)),
		putBlob(baseCtx, t, metadata, objects, []byte("old two"), now.Add(-48*time.Hour)),
	}
	ctx := meta.WithCleanupSnapshotLimits(baseCtx, meta.CleanupSnapshotLimits{MaxRecords: 1, MaxBytes: 1 << 20})
	_, err := cachegc.New(metadata, objects).Cleanup(ctx, cachegc.CleanupOptions{
		UnusedFor: 24 * time.Hour,
		Now:       now,
	})
	if !errors.Is(err, meta.ErrCleanupSnapshotLimit) {
		t.Fatalf("cleanup error = %v, want bounded snapshot limit error", err)
	}
	for _, digestValue := range digests {
		assertExists(baseCtx, t, objects, digestValue)
	}
	blobs, err := metadata.ListBlobs(baseCtx)
	if err != nil {
		t.Fatal(err)
	}
	if blobs.Len() != len(digests) {
		t.Fatalf("blobs after rejected cleanup = %d, want %d", blobs.Len(), len(digests))
	}
}

func TestCleanupManifestGraphRecordBudgetFailsBeforeDeletion(t *testing.T) {
	ctx := context.Background()
	metadata, objects := testStores(t)
	now := time.Now().UTC()
	body := []byte(`{"schemaVersion":2,"manifests":[{"digest":"child-one"},{"digest":"child-two"}]}`)
	putCleanupIndex(ctx, t, metadata, objects, body, now)
	candidate := putBlob(ctx, t, metadata, objects, []byte("old candidate"), now.Add(-48*time.Hour))
	ctx = meta.WithCleanupSnapshotLimits(ctx, meta.CleanupSnapshotLimits{MaxRecords: 2, MaxBytes: 1 << 20})
	_, err := cachegc.New(metadata, objects).Cleanup(ctx, cachegc.CleanupOptions{
		UnusedFor: 24 * time.Hour, PruneReferences: true, Now: now,
	})
	if !errors.Is(err, meta.ErrCleanupSnapshotLimit) {
		t.Fatalf("cleanup error = %v, want manifest graph record limit", err)
	}
	assertExists(context.Background(), t, objects, candidate)
	blobs, err := metadata.ListBlobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if blobs.Len() != 1 {
		t.Fatalf("blob rows after rejected cleanup = %d, want candidate preserved", blobs.Len())
	}
}

func TestCleanupManifestGraphByteBudgetFailsBeforeDeletion(t *testing.T) {
	ctx := context.Background()
	metadata, objects := testStores(t)
	now := time.Now().UTC()
	first := strings.Repeat("a", 600)
	second := strings.Repeat("b", 600)
	body := []byte(`{"schemaVersion":2,"manifests":[{"digest":"` + first + `"},{"digest":"` + second + `"}]}`)
	putCleanupIndex(ctx, t, metadata, objects, body, now)
	candidate := putBlob(ctx, t, metadata, objects, []byte("old candidate"), now.Add(-48*time.Hour))
	ctx = meta.WithCleanupSnapshotLimits(ctx, meta.CleanupSnapshotLimits{MaxRecords: 10, MaxBytes: 1024})
	_, err := cachegc.New(metadata, objects).Cleanup(ctx, cachegc.CleanupOptions{
		UnusedFor: 24 * time.Hour, PruneReferences: true, Now: now,
	})
	if !errors.Is(err, meta.ErrCleanupSnapshotLimit) {
		t.Fatalf("cleanup error = %v, want manifest graph byte limit", err)
	}
	assertExists(context.Background(), t, objects, candidate)
	blobs, err := metadata.ListBlobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if blobs.Len() != 1 {
		t.Fatalf("blob rows after rejected cleanup = %d, want candidate preserved", blobs.Len())
	}
}

func TestCleanupManifestGraphJSONBudgetFailsClosedInNonStrictMode(t *testing.T) {
	depth := jsonbudget.DefaultLimits().MaxDepth + 1
	nested := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
	invalidUTF8 := append([]byte(`{"schemaVersion":2,"manifests":[{"digest":"child"}],"ignored":"`), 0xff)
	invalidUTF8 = append(invalidUTF8, '"', '}')
	cases := []struct {
		name string
		body []byte
		want error
	}{
		{name: "shape limit", body: []byte(`{"schemaVersion":2,"manifests":[],"ignored":` + nested + `}`), want: jsonbudget.ErrLimit},
		{name: "invalid utf8 with valid child", body: invalidUTF8, want: jsonbudget.ErrInvalidJSON},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			metadata, objects := testStores(t)
			now := time.Now().UTC()
			putCleanupIndex(ctx, t, metadata, objects, test.body, now)
			candidate := putBlob(ctx, t, metadata, objects, []byte("old candidate"), now.Add(-48*time.Hour))
			_, err := cachegc.New(metadata, objects).Cleanup(ctx, cachegc.CleanupOptions{
				UnusedFor: 24 * time.Hour, Now: now,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("cleanup error = %v, want %v", err, test.want)
			}
			assertExists(context.Background(), t, objects, candidate)
			blobs, err := metadata.ListBlobs(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if blobs.Len() != 1 {
				t.Fatalf("blob rows after rejected cleanup = %d, want candidate preserved", blobs.Len())
			}
		})
	}
}

func putCleanupIndex(ctx context.Context, t *testing.T, metadata meta.Store, objects object.Store, body []byte, at time.Time) {
	t.Helper()
	digestValue := digest.SHA256.FromBytes(body).String()
	if _, err := objects.Put(ctx, digestValue, bytes.NewReader(body), object.PutOptions{ContentType: "application/vnd.oci.image.index.v1+json"}); err != nil {
		t.Fatal(err)
	}
	_, err := metadata.UpsertManifest(ctx, meta.ManifestRecord{
		Alias: "oci", Repository: "index", Reference: "latest", Digest: digestValue,
		MediaType: "application/vnd.oci.image.index.v1+json", Size: int64(len(body)), ObjectKey: digestValue,
		CreatedAt: at, UpdatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReconcileCapsRepairDetailsButRepairsEveryObject(t *testing.T) {
	ctx := context.Background()
	metadata, objects := testStores(t)
	bodies := [][]byte{[]byte("missing metadata one"), []byte("missing metadata two")}
	for _, body := range bodies {
		digestValue := digest.SHA256.FromBytes(body).String()
		if _, err := objects.Put(ctx, digestValue, bytes.NewReader(body), object.PutOptions{ContentType: "application/octet-stream"}); err != nil {
			t.Fatal(err)
		}
	}
	ctx = meta.WithCleanupSnapshotLimits(ctx, meta.CleanupSnapshotLimits{MaxRecords: 1, MaxBytes: 1 << 20})
	report, err := cachegc.New(metadata, objects).Reconcile(ctx, cachegc.ReconcileOptions{Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if report.RepairedMetadata != len(bodies) || len(report.RepairedDigests) != 1 || !report.RepairDetailsTruncated {
		t.Fatalf("reconcile report = %#v, want all repairs with one detail and truncation flag", report)
	}
	blobs, err := metadata.ListBlobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if blobs.Len() != len(bodies) {
		t.Fatalf("reconciled metadata rows = %d, want %d", blobs.Len(), len(bodies))
	}
}
