package api_test

import (
	"log/slog"
	"net/http"
	"testing"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/api"
)

func TestNewServerRegistersEndpoints(t *testing.T) {
	server := api.NewServer(api.Options{
		Endpoints: collectionlist.NewList[httpx.Endpoint](
			api.NewHealthEndpoint(),
			api.NewRegistryEndpoint(nil, nil, nil, nil, slog.Default()),
		),
	})

	assertRoute(t, server, http.MethodGet, "/healthz")
	assertRoute(t, server, http.MethodGet, "/v2")
	assertRoute(t, server, http.MethodHead, "/v2")
	assertRoute(t, server, http.MethodGet, "/v2/hub/library/alpine/manifests/latest")
	assertRoute(t, server, http.MethodHead, "/v2/hub/library/alpine/blobs/sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	assertRoute(t, server, http.MethodPost, "/v2/hub/library/alpine/blobs/uploads/")
}

func assertRoute(t *testing.T, server *api.Server, method, path string) {
	t.Helper()
	if !server.HasRoute(method, path) {
		t.Fatalf("expected route %s %s to match", method, path)
	}
}
