package upstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestClientGetBlobConcurrentFailoverClosesUnselectedBody(t *testing.T) {
	const digest = "sha256:abcdef0123456789"
	arrived := make(chan string, 2)
	ready := make(chan string, 2)
	canceled := make(chan string, 2)
	allowHeaders := make(chan struct{})
	allowBodies := make(chan struct{})
	first := newBlockedBlobMirror("first", digest, arrived, ready, canceled, allowHeaders, allowBodies)
	defer first.Close()
	second := newBlockedBlobMirror("second", digest, arrived, ready, canceled, allowHeaders, allowBodies)
	defer second.Close()
	client := newConcurrentBlobTestClient(first.URL, second.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	response := getConcurrentBlob(ctx, t, client, digest, arrived, ready, allowHeaders, allowBodies)
	winner := response.Headers.Get("X-Test-Mirror")
	assertUnselectedMirrorCanceled(t, winner, canceled)
	closeBody(t, response.Body)
	select {
	case name := <-canceled:
		if name != winner {
			t.Fatalf("caller close canceled %q, want selected mirror %q", name, winner)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatalf("selected body for %q did not cancel when caller closed it", winner)
	}
}

func TestClientGetBlobConcurrentTerminalErrorCancelsPendingMirror(t *testing.T) {
	const digest = "sha256:abcdef0123456789"
	arrived := make(chan struct{}, 2)
	canceled := make(chan struct{}, 1)
	terminal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		for range 2 {
			select {
			case <-arrived:
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer terminal.Close()
	pending := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		<-r.Context().Done()
		canceled <- struct{}{}
	}))
	defer pending.Close()
	client := newConcurrentBlobTestClient(terminal.URL, pending.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct{ err error }
	done := make(chan result, 1)
	go func() {
		_, err := client.GetBlob(ctx, upstream.GetBlobRequest{
			UpstreamAlias: "hub", Repo: "library/test", Digest: digest, Method: http.MethodGet,
		})
		done <- result{err: err}
	}()

	select {
	case got := <-done:
		requireErrorStatus(t, got.err, http.StatusUnauthorized)
	case <-time.After(time.Second):
		t.Fatal("GetBlob did not return the terminal unauthorized response")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("terminal error did not cancel the pending mirror")
	}
}

func newBlockedBlobMirror(
	name, digest string,
	arrived, ready, canceled chan<- string,
	allowHeaders, allowBodies <-chan struct{},
) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- name
		select {
		case <-r.Context().Done():
			canceled <- name
			return
		case <-allowHeaders:
		}
		w.Header().Set(distribution.HeaderDockerContentDigest, digest)
		w.Header().Set(distribution.HeaderContentLength, "4")
		w.Header().Set("X-Test-Mirror", name)
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		ready <- name
		select {
		case <-r.Context().Done():
			canceled <- name
			return
		case <-allowBodies:
		}
		<-r.Context().Done()
		canceled <- name
	}))
}

func newConcurrentBlobTestClient(firstURL, secondURL string) *upstream.Client {
	return newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors: []string{firstURL, secondURL},
			Blob: upstream.BlobConfig{
				MaxConcurrentAttempts: 2,
			},
		},
	})
}

func getConcurrentBlob(
	ctx context.Context,
	t *testing.T,
	client *upstream.Client,
	digest string,
	arrived <-chan string,
	ready <-chan string,
	allowHeaders chan struct{},
	allowBodies chan struct{},
) *upstream.BlobResponse {
	t.Helper()
	got := make(chan concurrentBlobResult, 1)
	go func() {
		response, err := client.GetBlob(ctx, upstream.GetBlobRequest{
			UpstreamAlias: "hub", Repo: "library/test", Digest: digest, Method: http.MethodGet,
		})
		got <- concurrentBlobResult{response: response, err: err}
	}()
	awaitBothMirrorRequests(t, arrived)
	close(allowHeaders)
	seen := awaitBothMirrorHeaders(t, ready)
	close(allowBodies)
	select {
	case result := <-got:
		if result.err != nil {
			t.Fatalf("GetBlob: %v", result.err)
		}
		if result.response == nil || result.response.Body == nil {
			t.Fatal("GetBlob returned no response body")
		}
		winner := result.response.Headers.Get("X-Test-Mirror")
		if !seen[winner] {
			t.Fatalf("selected mirror %q was not one of the concurrent attempts", winner)
		}
		return result.response
	case <-time.After(250 * time.Millisecond):
		t.Fatal("GetBlob did not select a successful mirror")
	}
	return nil
}

func awaitBothMirrorRequests(t *testing.T, arrived <-chan string) {
	t.Helper()
	for range 2 {
		select {
		case <-arrived:
		case <-time.After(time.Second):
			t.Fatal("both mirrors did not start concurrently")
		}
	}
}

func awaitBothMirrorHeaders(t *testing.T, ready <-chan string) map[string]bool {
	t.Helper()
	seen := make(map[string]bool, 2)
	for range 2 {
		select {
		case name := <-ready:
			seen[name] = true
		case <-time.After(time.Second):
			t.Fatal("both mirrors did not return headers concurrently")
		}
	}
	return seen
}

func assertUnselectedMirrorCanceled(t *testing.T, winner string, canceled <-chan string) {
	t.Helper()
	if winner != "first" && winner != "second" {
		t.Fatalf("selected mirror = %q, want first or second", winner)
	}
	loser := "first"
	if winner == loser {
		loser = "second"
	}
	select {
	case name := <-canceled:
		if name != loser {
			t.Fatalf("returned body for %q was canceled before caller closed it", name)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatalf("unselected body for %q was not promptly closed or canceled", loser)
	}
	select {
	case name := <-canceled:
		t.Fatalf("unexpected second canceled body before caller close: %q", name)
	case <-time.After(25 * time.Millisecond):
	}
}

type concurrentBlobResult struct {
	response *upstream.BlobResponse
	err      error
}
