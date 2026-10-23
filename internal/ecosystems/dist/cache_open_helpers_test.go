package dist_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/dist"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func newDistServiceWithObjects(upstreamURL string, metadata meta.Store, objects object.Store) *dist.Service {
	return dist.NewService(dist.ServiceDependencies{
		Config:   config.Config{Dist: config.DistEcosystemConfig{"gradle": {Registry: upstreamURL}}},
		Metadata: metadata,
		Objects:  objects,
	})
}

type delayedDistObjectStore struct {
	object.Store
	delay time.Duration
	gets  atomic.Int32
}

func (s *delayedDistObjectStore) Get(ctx context.Context, digest string, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	s.gets.Add(1)
	timer := time.NewTimer(s.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, nil, fmt.Errorf("wait for delayed dist object: %w", ctx.Err())
	case <-timer.C:
		reader, info, err := s.Store.Get(ctx, digest, opts)
		if err != nil {
			return reader, info, fmt.Errorf("get delayed dist object: %w", err)
		}
		return reader, info, nil
	}
}

type blockedDistObjectStore struct {
	object.Store
	started chan struct{}
}

func (s *blockedDistObjectStore) Get(ctx context.Context, _ string, _ object.GetOptions) (io.ReadCloser, *object.Info, error) {
	close(s.started)
	<-ctx.Done()
	return nil, nil, fmt.Errorf("get blocked dist object: %w", ctx.Err())
}

type transientDistObjectStore struct {
	object.Store
	failEvery bool
	gets      atomic.Int32
	puts      atomic.Int32
}

func (s *transientDistObjectStore) Get(ctx context.Context, digest string, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	call := s.gets.Add(1)
	if s.failEvery || call == 1 {
		return nil, nil, errors.New("temporary object store failure")
	}
	reader, info, err := s.Store.Get(ctx, digest, opts)
	if err != nil {
		return reader, info, fmt.Errorf("get dist test object: %w", err)
	}
	return reader, info, nil
}

func (s *transientDistObjectStore) Put(ctx context.Context, digest string, reader io.Reader, opts object.PutOptions) (*object.Info, error) {
	s.puts.Add(1)
	info, err := s.Store.Put(ctx, digest, reader, opts)
	if err != nil {
		return info, fmt.Errorf("put dist test object: %w", err)
	}
	return info, nil
}
