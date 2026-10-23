package container

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/stretchr/testify/require"
)

func TestPrewarmPlanImmediateWorkerClearsBeforeRelease(t *testing.T) {
	pools := worker.NewPoolsConfig(4, 0, slog.New(slog.DiscardHandler))
	t.Cleanup(pools.Close)
	endpoint := NewRegistryEndpointFromOptions(nil, prewarmPlanBlobs{}, nil, nil, slog.New(slog.DiscardHandler), RegistryEndpointOptions{Workers: pools})
	var consumed atomic.Int32
	for range 32 {
		permit := endpoint.admitManifestBlobFill(t.Context(), reference.Route{}, prewarmPlanManifest())
		require.NotNil(t, permit)
		work := &manifestBlobFillWork{
			endpoint: endpoint, route: reference.Route{Repo: "repo"}, mediaType: strings.Repeat("x", 1024), digest: "digest",
			task: func(context.Context) { consumed.Add(1) },
		}
		plan := newManifestBlobFillPlan(t.Context(), work, permit)
		require.NoError(t, manifestCompletion(t.Context(), nil, plan)())
		waitPrewarmAdmission(t, endpoint)
		assertPrewarmPlanCleared(t, plan)
		require.Nil(t, work.endpoint)
		require.Nil(t, work.task)
		require.Empty(t, work.mediaType)
		require.Empty(t, work.digest)
		require.Equal(t, reference.Route{}, work.route)
	}
	require.Equal(t, int32(32), consumed.Load(), "immediate tasks must execute rather than be skipped")
}

func TestPrewarmPlanCancellationAndSubmissionRace(t *testing.T) {
	endpoint := NewRegistryEndpoint(nil, prewarmPlanBlobs{}, nil, nil, slog.New(slog.DiscardHandler))
	for range 32 {
		ctx, cancel := context.WithCancel(t.Context())
		plan := endpoint.prepareManifestBlobFill(ctx, reference.Route{}, prewarmPlanManifest())
		require.NotNil(t, plan)
		var calls sync.WaitGroup
		calls.Go(cancel)
		calls.Go(func() { plan.submit(ctx) })
		calls.Go(plan.discard)
		calls.Wait()
		waitPrewarmAdmission(t, endpoint)
		assertPrewarmPlanCleared(t, plan)
	}
}
