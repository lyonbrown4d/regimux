package container_test

import (
	"log/slog"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

func newEndpointWithIOWorkers(
	t *testing.T,
	manifests cache.ManifestService,
	blobs cache.BlobService,
) *container.RegistryEndpoint {
	t.Helper()
	pools := worker.NewPoolsConfig(4, 0, slog.New(slog.DiscardHandler))
	t.Cleanup(pools.Close)
	return container.NewRegistryEndpointFromOptions(
		manifests,
		blobs,
		nil,
		nil,
		slog.New(slog.DiscardHandler),
		container.RegistryEndpointOptions{Workers: pools},
	)
}
