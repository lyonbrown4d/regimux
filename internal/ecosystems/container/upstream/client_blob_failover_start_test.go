package upstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

type blobStartResult struct {
	response *upstream.BlobResponse
	err      error
}

func TestClientGetBlobStartsConcurrentBlobFailover(t *testing.T) {
	t.Parallel()

	const digest = "sha256:abcdef0123456789"
	var slowRequests, fastRequests atomic.Int32
	var slowStartedOnce, fastEnteredOnce sync.Once
	slowStarted := make(chan struct{})
	fastEntered := make(chan struct{})
	slowRelease := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		slowRequests.Add(1)
		slowStartedOnce.Do(func() { close(slowStarted) })
		<-slowRelease
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fastRequests.Add(1)
		fastEnteredOnce.Do(func() { close(fastEntered) })
		select {
		case <-slowStarted:
		case <-r.Context().Done():
			return
		}
		w.Header().Set(distribution.HeaderDockerContentDigest, digest)
		w.Header().Set(distribution.HeaderContentLength, "4")
		writeString(t, w, "live")
	}))
	t.Cleanup(func() {
		close(slowRelease)
		cancel()
		fast.Close()
		slow.Close()
	})
	client := newConcurrentBlobTestClient(slow.URL, fast.URL)
	done := make(chan blobStartResult, 1)
	go func() {
		response, err := client.GetBlob(ctx, upstream.GetBlobRequest{
			UpstreamAlias: "hub", Repo: "library/nginx", Digest: digest,
		})
		done <- blobStartResult{response: response, err: err}
	}()

	waitForBlobStart(t, slowStarted, "slow endpoint did not receive request")
	waitForBlobStart(t, fastEntered, "fast endpoint was not attempted while slow endpoint was blocked")
	response := receiveStartedBlob(t, done)
	requireEqual(t, readAndClose(t, response.Body), "live", "fast endpoint body")
	requireEqual(t, fastRequests.Load(), int32(1), "fast endpoint requests")
	requireEqual(t, slowRequests.Load(), int32(1), "slow endpoint requests")
}

func waitForBlobStart(t *testing.T, started <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func receiveStartedBlob(t *testing.T, done <-chan blobStartResult) *upstream.BlobResponse {
	t.Helper()
	select {
	case result := <-done:
		requireNoError(t, result.err, "GetBlob")
		if result.response == nil || result.response.Body == nil {
			t.Fatal("GetBlob returned no response body")
		}
		return result.response
	case <-time.After(time.Second):
		t.Fatal("GetBlob did not return while the slow endpoint was blocked")
		return nil
	}
}
