package upstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestClientGetManifestRetriesTransientTransportError(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		request := requests.Add(1)
		requireEqual(t, r.URL.Path, "/v2/library/nginx/manifests/latest", "manifest path")
		if request == 1 {
			<-r.Context().Done()
			return
		}
		body := `{"schemaVersion":2}`
		w.Header().Set(distribution.HeaderDockerContentDigest, "sha256:abc")
		w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
		w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
		writeString(t, w, body)
	}))
	defer registryServer.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry: registryServer.URL,
			HTTP: upstream.HTTPConfig{
				Timeout: 100 * time.Millisecond,
				Retry: upstream.HTTPRetryConfig{
					Enabled:    true,
					MaxRetries: 1,
					WaitMin:    time.Millisecond,
					WaitMax:    time.Millisecond,
				},
			},
		},
	})

	resp, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Reference:     "latest",
	})
	requireNoError(t, err, "GetManifest")
	closeBody(t, resp.Body)
	requireEqual(t, requests.Load(), int32(2), "registry requests")
}
