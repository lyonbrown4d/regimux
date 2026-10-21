package maven_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestMavenMissingDoesNotHideMirrorTransportErrors(t *testing.T) {
	for _, failedHost := range []string{"primary.invalid", "mirror.invalid"} {
		t.Run(failedHost, func(t *testing.T) {
			failure := errors.New("synthetic mirror transport failure")
			transport := &missingMirrorTransport{failedHost: failedHost, failure: failure}
			objects, err := object.NewLocal(t.TempDir())
			requireNoError(t, "open objects", err)
			service := maven.NewService(maven.ServiceDependencies{
				Config: config.Config{
					Maven: map[string]config.DependencyUpstreamConfig{
						"central": {
							Registry: "https://primary.invalid",
							Mirrors:  []string{"https://mirror.invalid"},
						},
					},
				},
				Metadata: newTestMetadata(t.Context(), t),
				Objects:  objects,
				Client:   &http.Client{Transport: transport},
			})
			t.Cleanup(func() { requireNoError(t, "close Maven service", service.Close()) })
			resp, err := service.Get(t.Context(), maven.Request{Alias: "central", Tail: mirrorMissingTail})
			if resp != nil {
				closeBody(t, resp.Body)
				t.Fatalf("status = %d, want transport error", resp.Status)
			}
			if !errors.Is(err, failure) {
				t.Fatalf("error = %v, want wrapped mirror transport failure", err)
			}
			if got := transport.calls.Load(); got != 2 {
				t.Fatalf("requests = %d, want both configured mirrors checked", got)
			}
		})
	}
}

type missingMirrorTransport struct {
	failedHost string
	failure    error
	calls      atomic.Int32
}

func (transport *missingMirrorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	transport.calls.Add(1)
	if req.URL.Host == transport.failedHost {
		return nil, transport.failure
	}
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("missing from this mirror")),
	}, nil
}
