package api

import (
	"log/slog"
	"net/http"
	"testing"

	"github.com/arcgolabs/httpx"
)

func TestNewServerRegistersEndpoints(t *testing.T) {
	server := NewServer(Options{
		Endpoints: []httpx.Endpoint{
			NewHealthEndpoint(),
			NewRegistryEndpoint(nil, nil, nil, nil, slog.Default()),
		},
	})

	assertRoute(t, server, http.MethodGet, "/healthz")
	assertRoute(t, server, http.MethodGet, "/v2")
	assertRoute(t, server, http.MethodHead, "/v2")
	assertRoute(t, server, http.MethodGet, "/v2/hub/library/alpine/manifests/latest")
	assertRoute(t, server, http.MethodHead, "/v2/hub/library/alpine/blobs/sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
}

func assertRoute(t *testing.T, server *Server, method, path string) {
	t.Helper()
	if server == nil || server.runtime == nil {
		t.Fatal("server runtime is nil")
	}
	if _, ok := server.runtime.MatchRoute(method, path); !ok {
		t.Fatalf("expected route %s %s to match", method, path)
	}
}
