package dist_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/dist"
)

func TestDistTransferContinuationHeaderTimeoutIsBounded(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) != 1 {
			<-r.Context().Done()
			return
		}
		writeInterruptedDistResponse(t, w)
	}))
	t.Cleanup(server.Close)
	service := dist.NewService(dist.ServiceDependencies{
		Config: config.Config{Dist: config.DistEcosystemConfig{"gradle": {
			Registry: server.URL, HTTP: config.HTTPConfig{AttemptTimeout: 100 * time.Millisecond},
		}}}, Client: server.Client(),
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	_, err := service.Get(ctx, dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	if err == nil {
		t.Fatal("continuation without headers unexpectedly succeeded")
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("header attempt did not honor 100ms budget: %s", elapsed)
	}
	if ctx.Err() != nil {
		t.Fatalf("header attempt canceled parent: %v", ctx.Err())
	}
	if requests.Load() < 2 {
		t.Fatalf("recovery did not request continuation: requests=%d", requests.Load())
	}
}
