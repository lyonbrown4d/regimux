package upstream_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
)

func TestBlobTransferContinuationHeaderTimeoutIsBounded(t *testing.T) {
	const content = "0123456789abcdef"
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content)))
	var requests atomic.Int32
	server := httptest.NewServer(headerStallingBlobHandler(t, content, digest, &requests))
	t.Cleanup(server.Close)
	cfg := upstream.ConfigFromUpstreamConfig("hub", config.UpstreamConfig{
		Registry: server.URL, HTTP: config.HTTPConfig{AttemptTimeout: 100 * time.Millisecond},
	})
	client := newTestClient(map[string]upstream.Config{"hub": cfg})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	response, err := client.GetBlob(ctx, upstream.GetBlobRequest{UpstreamAlias: "hub", Repo: "library/test", Digest: digest, Method: http.MethodGet})
	requireNoError(t, err, "get truncated blob")
	_, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	assertHeaderTimeoutResult(ctx, t, started, readErr, closeErr, requests.Load())
}

func headerStallingBlobHandler(t *testing.T, content, digest string, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) != 1 {
			<-r.Context().Done()
			return
		}
		w.Header().Set("ETag", `"stable"`)
		w.Header().Set("Docker-Content-Digest", digest)
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		if _, err := w.Write([]byte(content[:5])); err != nil {
			t.Error(err)
			return
		}
		controller := http.NewResponseController(w)
		if err := controller.Flush(); err != nil {
			t.Error(err)
			return
		}
		connection, _, err := controller.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	})
}

func assertHeaderTimeoutResult(ctx context.Context, t *testing.T, started time.Time, readErr, closeErr error, requests int32) {
	t.Helper()
	if readErr == nil {
		t.Fatal("continuation without headers unexpectedly succeeded")
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("header attempt did not honor 100ms budget: %s", elapsed)
	}
	if ctx.Err() != nil {
		t.Fatalf("header attempt canceled parent: %v", ctx.Err())
	}
	if requests < 2 {
		t.Fatalf("recovery did not request continuation: requests=%d", requests)
	}
	if closeErr != nil {
		t.Logf("close failed continuation: %v", closeErr)
	}
}
