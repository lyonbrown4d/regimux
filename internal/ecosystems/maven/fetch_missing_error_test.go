//nolint:testpackage // Transport error assertions inspect the physical endpoint boundary.
package maven

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestMavenMissingDoesNotHideMirrorTransportErrors(t *testing.T) {
	for _, failedHost := range []string{"primary.invalid", "mirror.invalid"} {
		t.Run(failedHost, func(t *testing.T) {
			failure := errors.New("synthetic mirror transport failure")
			transport := &missingMirrorTransport{failedHost: failedHost, failure: failure}
			service := &Service{client: &http.Client{Transport: transport}}
			fetched, err := service.fetchPhysical(
				t.Context(),
				config.UpstreamConfig{
					Registry: "https://primary.invalid",
					Mirrors:  []string{"https://mirror.invalid"},
				},
				"central",
				Route{UpstreamTail: "com/acme/demo/1.0/demo-1.0.pom"},
				http.MethodGet,
			)
			if fetched != nil {
				closeReadCloser(fetched.body, nil, "close unexpected mirror response")
				t.Fatalf("status = %d, want transport error", fetched.status)
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
