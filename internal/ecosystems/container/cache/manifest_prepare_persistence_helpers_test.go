package cache_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type trackedManifestPersistenceStore struct {
	meta.Store
	upstreamPulls   atomic.Int64
	manifests       atomic.Int64
	tags            atomic.Int64
	pulls           atomic.Int64
	upstreamEntered chan struct{}
	upstreamRelease chan struct{}
}

type failingManifestObjectStore struct{ object.Store }

func (s failingManifestObjectStore) Put(context.Context, string, io.Reader, object.PutOptions) (*object.Info, error) {
	return nil, errors.New("test object write failure")
}

type failingManifestTagStore struct{ meta.Store }

func (s failingManifestTagStore) UpsertTag(context.Context, meta.TagRecord) (*meta.TagRecord, error) {
	return nil, errors.New("test tag write failure")
}

type alternatingManifestObjectStore struct {
	object.Store
	gets atomic.Int64
}

func (s *alternatingManifestObjectStore) Get(ctx context.Context, digest string, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	if s.gets.Add(1)%2 == 1 {
		return nil, nil, object.ErrNotFound
	}
	body, info, err := s.Store.Get(ctx, digest, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("get alternating manifest object: %w", err)
	}
	return body, info, nil
}

type firstObjectReadMissStore struct {
	object.Store
	gets atomic.Int64
}

func requireManifestPreparer(t *testing.T, proxy *cache.Proxy) cache.ManifestPreparer {
	t.Helper()
	preparer, ok := proxy.Manifests().(cache.ManifestPreparer)
	if !ok {
		t.Fatal("manifest proxy does not implement ManifestPreparer")
	}
	return preparer
}

func completeManifestResponse(ctx context.Context, t *testing.T, complete cache.ManifestCompletion) {
	t.Helper()
	if err := complete.CompleteResponse(ctx); err != nil {
		t.Errorf("complete prepared manifest response: %v", err)
	}
}

func (s *firstObjectReadMissStore) Get(ctx context.Context, digest string, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	if s.gets.Add(1) == 1 {
		return nil, nil, object.ErrNotFound
	}
	body, info, err := s.Store.Get(ctx, digest, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("get first-read manifest object: %w", err)
	}
	return body, info, nil
}

func (s *trackedManifestPersistenceStore) RecordUpstreamPull(ctx context.Context, key meta.PullKey, at time.Time) (*meta.PullRecord, error) {
	s.upstreamPulls.Add(1)
	record, err := s.Store.RecordUpstreamPull(ctx, key, at)
	if s.upstreamEntered != nil {
		s.upstreamEntered <- struct{}{}
		<-s.upstreamRelease
	}
	if err != nil {
		return nil, fmt.Errorf("record tracked upstream manifest pull: %w", err)
	}
	return record, nil
}

func (s *trackedManifestPersistenceStore) UpsertManifest(ctx context.Context, record meta.ManifestRecord) (*meta.ManifestRecord, error) {
	s.manifests.Add(1)
	manifest, err := s.Store.UpsertManifest(ctx, record)
	if err != nil {
		return nil, fmt.Errorf("upsert tracked manifest: %w", err)
	}
	return manifest, nil
}

func (s *trackedManifestPersistenceStore) UpsertTag(ctx context.Context, record meta.TagRecord) (*meta.TagRecord, error) {
	s.tags.Add(1)
	tag, err := s.Store.UpsertTag(ctx, record)
	if err != nil {
		return nil, fmt.Errorf("upsert tracked manifest tag: %w", err)
	}
	return tag, nil
}

func (s *trackedManifestPersistenceStore) RecordPull(ctx context.Context, key meta.PullKey, at time.Time) (*meta.PullRecord, error) {
	s.pulls.Add(1)
	pull, err := s.Store.RecordPull(ctx, key, at)
	if err != nil {
		return nil, fmt.Errorf("record tracked manifest pull: %w", err)
	}
	return pull, nil
}
