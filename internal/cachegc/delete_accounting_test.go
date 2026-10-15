package cachegc_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cachegc"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestCleanupDoesNotCountObjectWhenAccessChangesAfterStat(t *testing.T) {
	ctx := context.Background()
	metadata, objects := testStores(t)
	now := time.Now().UTC().Add(72 * time.Hour)
	digestValue := putBlob(ctx, t, metadata, objects, []byte("touched by stat"), now.Add(-72*time.Hour))

	wrapped := touchDuringStatStore{Store: objects, metadata: metadata, accessedAt: now}
	report, err := cachegc.New(metadata, wrapped).Cleanup(ctx, cachegc.CleanupOptions{
		UnusedFor:       24 * time.Hour,
		PruneReferences: true,
		Now:             now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.DeletedBlobs != 0 || report.BytesDeleted != 0 || report.MissingObjects != 0 {
		t.Fatalf("skipped object was counted as reclaimed: %+v", report)
	}
	assertExists(ctx, t, objects, digestValue)
}

type touchDuringStatStore struct {
	object.Store
	metadata   meta.Store
	accessedAt time.Time
}

func (s touchDuringStatStore) Stat(ctx context.Context, digest string) (*object.Info, error) {
	info, err := s.Store.Stat(ctx, digest)
	if err != nil {
		return nil, fmt.Errorf("stat object before touch: %w", err)
	}
	blob, ok, err := s.metadata.Blob(ctx, meta.BlobKey{Digest: digest})
	if err != nil {
		return nil, fmt.Errorf("read blob metadata during stat: %w", err)
	}
	if !ok {
		return nil, errors.New("test blob metadata disappeared during Stat")
	}
	blob.LastAccessAt = s.accessedAt
	if _, err = s.metadata.UpsertBlob(ctx, *blob); err != nil {
		return nil, fmt.Errorf("touch blob metadata during stat: %w", err)
	}
	return info, nil
}
