package upstream_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
)

func TestBlobTransferRecoversInterruptedHTTPBody(t *testing.T) {
	const content = "0123456789abcdef"
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content)))
	var requests atomic.Int32
	server := httptest.NewServer(interruptedBlobHandler(t, content, digest, &requests))
	t.Cleanup(server.Close)
	client := newTestClient(map[string]upstream.Config{"hub": {
		Registry: server.URL, HTTP: upstream.HTTPConfig{AttemptTimeout: 100 * time.Millisecond},
	}})
	response, err := client.GetBlob(context.Background(), upstream.GetBlobRequest{UpstreamAlias: "hub", Repo: "library/test", Digest: digest, Method: http.MethodGet})
	requireNoError(t, err, "get interrupted blob")
	if got := readAndClose(t, response.Body); got != content {
		t.Fatalf("body=%q want=%q", got, content)
	}
	requireEqual(t, requests.Load(), int32(2), "requests")
}

func interruptedBlobHandler(t *testing.T, content, digest string, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", digest)
		w.Header().Set("ETag", `"stable"`)
		if requests.Add(1) == 1 {
			serveInterruptedBlob(t, w, content)
			return
		}
		serveBlobContinuation(t, w, r, content)
	})
}

func serveInterruptedBlob(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
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
}

func serveBlobContinuation(t *testing.T, w http.ResponseWriter, r *http.Request, content string) {
	t.Helper()
	if got := r.Header.Get("Range"); got != "bytes=5-15" {
		t.Errorf("recovery Range=%q", got)
	}
	w.Header().Set("Content-Range", "bytes 5-15/16")
	w.Header().Set("Content-Length", "11")
	w.WriteHeader(http.StatusPartialContent)
	if err := http.NewResponseController(w).Flush(); err != nil {
		t.Error(err)
		return
	}
	select {
	case <-r.Context().Done():
		return
	case <-time.After(200 * time.Millisecond):
	}
	if _, err := w.Write([]byte(content[5:])); err != nil {
		t.Error(err)
	}
}
