package pypi_test

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type observedPackageObjectStore struct {
	object.Store
	mu           sync.Mutex
	observations []*packageObjectReadObservation
}

type packageObjectReadObservation struct {
	openDuration time.Duration
	bytes        int64
	maxGap       time.Duration
	readErr      string
	contextErr   string
	lastRead     time.Time
	mu           sync.Mutex
}

func (s *observedPackageObjectStore) Get(ctx context.Context, digest string, options object.GetOptions) (io.ReadCloser, *object.Info, error) {
	started := time.Now()
	reader, info, err := s.Store.Get(ctx, digest, options)
	if err != nil {
		return nil, nil, fmt.Errorf("open observed package object: %w", err)
	}
	observation := &packageObjectReadObservation{openDuration: time.Since(started)}
	s.mu.Lock()
	s.observations = append(s.observations, observation)
	s.mu.Unlock()
	return &observedPackageObjectReader{ReadCloser: reader, ctx: ctx, observation: observation}, info, nil
}

func (s *observedPackageObjectStore) snapshot() []*packageObjectReadObservation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*packageObjectReadObservation(nil), s.observations...)
}

type observedPackageObjectReader struct {
	io.ReadCloser
	ctx         context.Context
	observation *packageObjectReadObservation
}

func (r *observedPackageObjectReader) Read(buffer []byte) (int, error) {
	now := time.Now()
	n, err := r.ReadCloser.Read(buffer)
	r.observation.mu.Lock()
	defer r.observation.mu.Unlock()
	if !r.observation.lastRead.IsZero() {
		gap := now.Sub(r.observation.lastRead)
		if gap > r.observation.maxGap {
			r.observation.maxGap = gap
		}
	}
	r.observation.lastRead = now
	r.observation.bytes += int64(n)
	if err != nil && err != io.EOF {
		r.observation.readErr = fmt.Sprintf("%T: %v", err, err)
		if ctxErr := r.ctx.Err(); ctxErr != nil {
			r.observation.contextErr = fmt.Sprintf("%T: %v", ctxErr, ctxErr)
		}
	}
	if err == io.EOF {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read observed package object: %w", err)
	}
	return n, nil
}

type blockedPackageReadStore struct {
	object.Store
	started chan struct{}
}

func (s *blockedPackageReadStore) Get(ctx context.Context, digest string, options object.GetOptions) (io.ReadCloser, *object.Info, error) {
	reader, info, err := s.Store.Get(ctx, digest, options)
	if err != nil {
		return nil, nil, fmt.Errorf("open blocked package test object: %w", err)
	}
	return &blockedPackageObjectReader{ReadCloser: reader, ctx: ctx, started: s.started}, info, nil
}

type blockedPackageObjectReader struct {
	io.ReadCloser
	ctx     context.Context
	started chan struct{}
	once    sync.Once
}

func (r *blockedPackageObjectReader) Read([]byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	<-r.ctx.Done()
	return 0, fmt.Errorf("blocked package read: %w", r.ctx.Err())
}

func newObservedPackageService(upstreamURL string, metadata meta.Store, objects object.Store) *pypi.Service {
	return pypi.NewService(pypi.ServiceDependencies{
		Config: config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {
			Registry: upstreamURL, HTTP: config.HTTPConfig{AttemptTimeout: 100 * time.Millisecond},
		}}},
		Metadata: metadata,
		Objects:  objects,
	})
}

func closePackageBody(body io.Closer) error {
	if body == nil {
		return nil
	}
	if err := body.Close(); err != nil {
		return fmt.Errorf("close package response body: %w", err)
	}
	return nil
}
