package cache_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestPreparedColdManifestWriteFailurePreservesBestEffortSequence(t *testing.T) {
	t.Run("manifest failure skips tag", func(t *testing.T) {
		assertPreparedColdManifestFailure(t, "manifest", []string{"upstream", "manifest", "pull"})
	})
	t.Run("upstream failure continues metadata", func(t *testing.T) {
		assertPreparedColdManifestFailure(t, "upstream", []string{"upstream", "manifest", "tag", "pull"})
	})
}

func assertPreparedColdManifestFailure(t *testing.T, failOperation string, want []string) {
	t.Helper()
	metadata, objects := newTestStores(t)
	tracked := &coldManifestWriteFailureStore{Store: metadata, failOperation: failOperation}
	proxy := newTestProxy(
		readyManifestPrepareClient([]byte(`{"schemaVersion":2}`)), tracked, objects,
		backend.NewMemory(backend.MemoryOptions{}), config.Config{},
	)
	service := requireManifestPreparer(t, proxy)
	_, complete, err := service.Prepare(t.Context(), cancellationManifestRequest())
	if err != nil || complete == nil {
		t.Fatalf("prepare cold manifest: completion=%t error=%v", complete != nil, err)
	}
	for range 2 {
		if err := complete.CompleteResponse(t.Context()); err != nil {
			t.Fatalf("best-effort completion propagated %s failure: %v", failOperation, err)
		}
	}
	if !reflect.DeepEqual(tracked.operations, want) {
		t.Fatalf("metadata operation order=%v, want %v", tracked.operations, want)
	}
	if tracked.pullWrites != 1 {
		t.Errorf("per-HTTP pull writes=%d, want exactly one", tracked.pullWrites)
	}
}

type coldManifestWriteFailureStore struct {
	meta.Store
	failOperation string
	operations    []string
	pullWrites    int
}

func (s *coldManifestWriteFailureStore) RecordUpstreamPull(ctx context.Context, key meta.PullKey, at time.Time) (*meta.PullRecord, error) {
	s.operations = append(s.operations, "upstream")
	if s.failOperation == "upstream" {
		return nil, errors.New("injected upstream write failure")
	}
	record, err := s.Store.RecordUpstreamPull(ctx, key, at)
	if err != nil {
		return nil, fmt.Errorf("record cold manifest upstream pull: %w", err)
	}
	return record, nil
}

func (s *coldManifestWriteFailureStore) UpsertManifest(ctx context.Context, record meta.ManifestRecord) (*meta.ManifestRecord, error) {
	s.operations = append(s.operations, "manifest")
	if s.failOperation == "manifest" {
		return nil, errors.New("injected manifest write failure")
	}
	stored, err := s.Store.UpsertManifest(ctx, record)
	if err != nil {
		return nil, fmt.Errorf("upsert cold manifest: %w", err)
	}
	return stored, nil
}

func (s *coldManifestWriteFailureStore) UpsertTag(ctx context.Context, record meta.TagRecord) (*meta.TagRecord, error) {
	s.operations = append(s.operations, "tag")
	tag, err := s.Store.UpsertTag(ctx, record)
	if err != nil {
		return nil, fmt.Errorf("upsert cold manifest tag: %w", err)
	}
	return tag, nil
}

func (s *coldManifestWriteFailureStore) RecordPull(ctx context.Context, key meta.PullKey, at time.Time) (*meta.PullRecord, error) {
	s.operations = append(s.operations, "pull")
	s.pullWrites++
	pull, err := s.Store.RecordPull(ctx, key, at)
	if err != nil {
		return nil, fmt.Errorf("record cold manifest pull: %w", err)
	}
	return pull, nil
}
