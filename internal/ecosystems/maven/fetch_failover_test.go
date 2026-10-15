package maven

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestServiceFailsOverWhenMavenEndpointAttemptTimesOut(t *testing.T) {
	ctx := context.Background()
	var slowRequests atomic.Int64
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slowRequests.Add(1)
		select {
		case <-time.After(200 * time.Millisecond):
			w.WriteHeader(http.StatusBadGateway)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)

	var mirrorRequests atomic.Int64
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mirrorRequests.Add(1)
		writeFetchResponse(t, w, "healthy mirror")
	}))
	t.Cleanup(mirror.Close)

	service := &Service{factory: clientfactory.New(nil)}
	resp, err := service.fetchPhysical(ctx, config.UpstreamConfig{
		Registry: slow.URL,
		Mirrors:  []string{mirror.URL},
		HTTP: config.HTTPConfig{
			AttemptTimeout: 20 * time.Millisecond,
		},
	}, "central", Route{UpstreamTail: "com/acme/demo/1.0/demo-1.0.jar"}, http.MethodGet)
	if err != nil {
		t.Fatalf("fetch after endpoint timeout: %v", err)
	}
	assertFetchBody(t, resp, "healthy mirror")
	if slowRequests.Load() != 1 || mirrorRequests.Load() != 1 {
		t.Fatalf("requests = slow:%d mirror:%d, want one request per endpoint", slowRequests.Load(), mirrorRequests.Load())
	}
}

func TestServiceFailsOverWhenMavenEndpointReturnsRetryableStatus(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			ctx := context.Background()
			var primaryRequests atomic.Int64
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				primaryRequests.Add(1)
				w.WriteHeader(status)
				writeFetchResponse(t, w, "retryable upstream")
			}))
			t.Cleanup(primary.Close)

			var mirrorRequests atomic.Int64
			mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				mirrorRequests.Add(1)
				writeFetchResponse(t, w, "healthy mirror")
			}))
			t.Cleanup(mirror.Close)

			service := &Service{factory: clientfactory.New(nil)}
			resp, err := service.fetchPhysical(ctx, config.UpstreamConfig{
				Registry: primary.URL,
				Mirrors:  []string{mirror.URL},
			}, "central", Route{UpstreamTail: "com/acme/demo/1.0/demo-1.0.jar"}, http.MethodGet)
			if err != nil {
				t.Fatalf("fetch after retryable status: %v", err)
			}
			assertFetchBody(t, resp, "healthy mirror")
			if primaryRequests.Load() != 1 || mirrorRequests.Load() != 1 {
				t.Fatalf("requests = primary:%d mirror:%d, want one request per endpoint", primaryRequests.Load(), mirrorRequests.Load())
			}
		})
	}
}

func TestFetchPhysicalStopsWhenCallerContextIsCanceled(t *testing.T) {
	started := make(chan struct{})
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(primary.Close)

	var mirrorRequests atomic.Int64
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mirrorRequests.Add(1)
		writeFetchResponse(t, w, "should not be requested")
	}))
	t.Cleanup(mirror.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := &Service{factory: clientfactory.New(nil)}
	result := make(chan error, 1)
	go func() {
		_, err := service.fetchPhysical(ctx, config.UpstreamConfig{
			Registry: primary.URL,
			Mirrors:  []string{mirror.URL},
		}, "central", Route{UpstreamTail: "com/acme/demo/1.0/demo-1.0.jar"}, http.MethodGet)
		result <- err
	}()
	<-started
	cancel()
	if err := <-result; err == nil {
		t.Fatal("fetchPhysical succeeded after caller context cancellation")
	}
	if mirrorRequests.Load() != 0 {
		t.Fatalf("mirror requests = %d, want 0 after caller cancellation", mirrorRequests.Load())
	}
}

func writeFetchResponse(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func assertFetchBody(t *testing.T, fetched *upstreamFetch, want string) {
	t.Helper()
	if fetched == nil || fetched.body == nil {
		t.Fatal("fetched response body is empty")
	}
	defer fetched.body.Close()
	body, err := io.ReadAll(fetched.body)
	if err != nil {
		t.Fatalf("read fetched body: %v", err)
	}
	if string(body) != want {
		t.Fatalf("fetched body = %q, want %q", string(body), want)
	}
}

func TestDefaultMavenAttemptTimeoutIsBounded(t *testing.T) {
	cfg, err := config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if got := cfg.Maven["central"].HTTP.AttemptTimeout; got != 10*time.Second {
		t.Fatalf("maven attempt timeout = %s, want 10s", got)
	}
}
