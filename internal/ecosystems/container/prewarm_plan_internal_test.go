package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/stretchr/testify/require"
)

func TestPrewarmPlansRemainBoundedWhileManifestSQLWaits(t *testing.T) {
	const requests = 16
	endpoint := NewRegistryEndpoint(nil, prewarmPlanBlobs{}, nil, nil, slog.New(slog.DiscardHandler))
	completion := &prewarmSQLWait{entered: make(chan struct{}, requests), release: make(chan struct{})}
	var release sync.Once
	manifest := prewarmPlanManifest()
	var retained atomic.Int32
	var finished sync.WaitGroup
	t.Cleanup(func() {
		release.Do(func() { close(completion.release) })
		finished.Wait()
	})
	for range requests {
		finished.Go(func() {
			endpoint.metadataResponse(t.Context(), func() (*registryOutput, func() error) {
				fill := endpoint.prepareManifestBlobFill(t.Context(), reference.Route{}, manifest)
				if fill != nil {
					retained.Add(1)
				}
				return &registryOutput{Status: http.StatusOK}, manifestCompletion(t.Context(), completion, fill)
			})
		})
	}
	for range requests {
		select {
		case <-completion.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("SQL waits prevented later metadata requests from leaving admission")
		}
	}
	require.Equal(t, int32(4), retained.Load(), "SQL waits retained more than four decoded descriptor plans")
	release.Do(func() { close(completion.release) })
	finished.Wait()
}

func TestPrewarmPlanCapacitySkipsParsing(t *testing.T) {
	handler := &prewarmWarningHandler{}
	endpoint := NewRegistryEndpoint(nil, prewarmPlanBlobs{}, nil, nil, slog.New(handler))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	for range 4 {
		require.NotNil(t, endpoint.prepareManifestBlobFill(ctx, reference.Route{}, prewarmPlanManifest()))
	}
	malformed := prewarmPlanManifest()
	malformed.Body = []byte(`{"layers":`)
	require.Nil(t, endpoint.prepareManifestBlobFill(ctx, reference.Route{}, malformed))
	require.Zero(t, handler.warnings.Load(), "full admission still decoded another manifest")
}

func prewarmPlanManifest() *cache.CachedManifest {
	body := `{"schemaVersion":2,"layers":[{"mediaType":"` + strings.Repeat("x", 256<<10) + `","digest":"sha256:` + strings.Repeat("a", 64) + `","size":1}]}`
	return &cache.CachedManifest{MediaType: distribution.MediaTypeOCIManifest, Digest: "sha256:" + strings.Repeat("b", 64), Body: []byte(body), Cache: cache.CacheMiss}
}

type prewarmSQLWait struct {
	entered        chan struct{}
	release        chan struct{}
	keepSQLPending bool
}

func (*prewarmSQLWait) Finalize(context.Context) error { return nil }

func (s *prewarmSQLWait) CompleteResponse(ctx context.Context) error {
	s.entered <- struct{}{}
	if s.keepSQLPending {
		<-s.release
		return nil
	}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("complete prewarm SQL wait: %w", ctx.Err())
	}
}

type prewarmPlanBlobs struct{}

func (prewarmPlanBlobs) Get(context.Context, cache.BlobRequest) (*cache.BlobReadResult, error) {
	return nil, errors.New("prewarm worker must not run without an I/O pool")
}

type prewarmWarningHandler struct{ warnings atomic.Int32 }

func (*prewarmWarningHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *prewarmWarningHandler) Handle(_ context.Context, record slog.Record) error {
	if record.Level >= slog.LevelWarn {
		h.warnings.Add(1)
	}
	return nil
}

func (h *prewarmWarningHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *prewarmWarningHandler) WithGroup(string) slog.Handler      { return h }
