package container

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/stretchr/testify/require"
)

func TestPrewarmPlanReleasesOnParseFailureAndCacheHit(t *testing.T) {
	endpoint := NewRegistryEndpoint(nil, prewarmPlanBlobs{}, nil, nil, slog.New(slog.DiscardHandler))
	for range 8 {
		manifest := prewarmPlanManifest()
		manifest.Body = []byte(`{"layers":`)
		require.Nil(t, endpoint.prepareManifestBlobFill(t.Context(), reference.Route{}, manifest))
		manifest.Cache = cache.CacheHit
		require.Nil(t, endpoint.prepareManifestBlobFill(t.Context(), reference.Route{}, manifest))
	}
	waitPrewarmAdmission(t, endpoint)
}

func TestPrewarmPlanCancellationClearsSQLWaitPayload(t *testing.T) {
	endpoint := NewRegistryEndpoint(nil, prewarmPlanBlobs{}, nil, nil, slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	completion := &prewarmSQLWait{entered: make(chan struct{}, 4), release: make(chan struct{}), keepSQLPending: true}
	var pending sync.WaitGroup
	t.Cleanup(func() {
		close(completion.release)
		pending.Wait()
	})
	plans := make([]*manifestBlobFillPlan, 0, 4)
	for range 4 {
		plan := endpoint.prepareManifestBlobFill(ctx, reference.Route{Repo: "repo"}, prewarmPlanManifest())
		require.NotNil(t, plan)
		plans = append(plans, plan)
		pending.Go(func() {
			if err := manifestCompletion(ctx, completion, plan)(); err != nil {
				t.Errorf("complete canceled SQL wait: %v", err)
			}
		})
	}
	for range 4 {
		<-completion.entered
	}
	cancel()
	waitPrewarmAdmission(t, endpoint)
	for _, plan := range plans {
		assertPrewarmPlanCleared(t, plan)
		plan.discard()
		plan.submit(ctx)
	}
	waitPrewarmAdmission(t, endpoint)
}

func TestPrewarmPlanCompletionFailureReleasesAdmission(t *testing.T) {
	endpoint := NewRegistryEndpoint(nil, prewarmPlanBlobs{}, nil, nil, slog.New(slog.DiscardHandler))
	for range 8 {
		plan := endpoint.prepareManifestBlobFill(t.Context(), reference.Route{}, prewarmPlanManifest())
		require.NotNil(t, plan)
		require.Error(t, manifestCompletion(t.Context(), failedPrewarmCompletion{}, plan)())
		assertPrewarmPlanCleared(t, plan)
	}
	waitPrewarmAdmission(t, endpoint)
}

func TestPrewarmPlanWorkerSubmissionReleasesOnUnavailableOrRejected(t *testing.T) {
	for _, mode := range []string{"missing", "disabled", "saturated", "rejected"} {
		t.Run(mode, func(t *testing.T) { assertPrewarmSubmissionRelease(t, mode) })
	}
}

func assertPrewarmSubmissionRelease(t *testing.T, mode string) {
	t.Helper()
	endpoint := NewRegistryEndpoint(nil, prewarmPlanBlobs{}, nil, nil, slog.New(slog.DiscardHandler))
	if mode != "missing" {
		endpoint.workers = worker.NewPoolsConfig(1, 0, slog.New(slog.DiscardHandler))
		t.Cleanup(endpoint.workers.Close)
	}
	configurePrewarmPool(t, endpoint, mode)
	for range 8 {
		plan := endpoint.prepareManifestBlobFill(t.Context(), reference.Route{}, prewarmPlanManifest())
		require.NotNil(t, plan)
		require.NoError(t, manifestCompletion(t.Context(), nil, plan)())
		assertPrewarmPlanCleared(t, plan)
	}
	waitPrewarmAdmission(t, endpoint)
}

func configurePrewarmPool(t *testing.T, endpoint *RegistryEndpoint, mode string) {
	t.Helper()
	switch mode {
	case "disabled":
		endpoint.workers.Close()
	case "rejected":
		endpoint.workers.IOPool().Release()
		require.Positive(t, endpoint.workers.IOPool().Free(), "rejection must reach Submit rather than saturation")
	case "saturated":
		entered, release := make(chan struct{}), make(chan struct{})
		t.Cleanup(func() { close(release) })
		require.NoError(t, endpoint.workers.IOPool().Submit(func() { close(entered); <-release }))
		<-entered
	case "missing":
	}
}

func TestPrewarmPlanAdmissionCoversWorkerConsumptionAndFailure(t *testing.T) {
	source := &blockedPrewarmBlobs{entered: make(chan struct{}, 4), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(source.release) }) })
	pools := worker.NewPoolsConfig(4, 0, slog.New(slog.DiscardHandler))
	t.Cleanup(pools.Close)
	endpoint := NewRegistryEndpointFromOptions(nil, source, nil, nil, slog.New(slog.DiscardHandler), RegistryEndpointOptions{Workers: pools})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	for range 4 {
		plan := endpoint.prepareManifestBlobFill(ctx, reference.Route{}, prewarmPlanManifest())
		require.NotNil(t, plan)
		require.NoError(t, manifestCompletion(ctx, nil, plan)())
		assertPrewarmPlanCleared(t, plan)
	}
	for range 4 {
		select {
		case <-source.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("worker did not consume the admitted prewarm task")
		}
	}
	cancel()
	require.Nil(t, endpoint.prepareManifestBlobFill(t.Context(), reference.Route{}, prewarmPlanManifest()), "worker released admission before consuming descriptors")
	release.Do(func() { close(source.release) })
	waitPrewarmAdmission(t, endpoint)
}

func waitPrewarmAdmission(t *testing.T, endpoint *RegistryEndpoint) {
	t.Helper()
	require.Eventually(t, func() bool {
		if !endpoint.prewarmGate.TryAcquire(4) {
			return false
		}
		endpoint.prewarmGate.Release(4)
		return true
	}, 2*time.Second, time.Millisecond, "prewarm permits were not released")
}

func assertPrewarmPlanCleared(t *testing.T, plan *manifestBlobFillPlan) {
	t.Helper()
	plan.mu.Lock()
	defer plan.mu.Unlock()
	require.Nil(t, plan.work)
	require.Nil(t, plan.stopCancel)
	require.Nil(t, plan.release)
}

type failedPrewarmCompletion struct{}

func (failedPrewarmCompletion) Finalize(context.Context) error { return nil }
func (failedPrewarmCompletion) CompleteResponse(context.Context) error {
	return errors.New("canonical SQL write failed")
}

type blockedPrewarmBlobs struct {
	entered chan struct{}
	release chan struct{}
}

func (b *blockedPrewarmBlobs) Get(ctx context.Context, _ cache.BlobRequest) (*cache.BlobReadResult, error) {
	b.entered <- struct{}{}
	select {
	case <-b.release:
		return nil, errors.New("upstream prewarm failure")
	case <-ctx.Done():
		return nil, errors.New("request cancellation leaked into an accepted prewarm task")
	}
}
