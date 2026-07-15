package api_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/golang"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/npm"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestNewServerRegistersEndpoints(t *testing.T) {
	server := api.NewServer(api.Options{
		Endpoints: collectionlist.NewList[httpx.Endpoint](
			container.NewRegistryEndpoint(nil, nil, nil, nil, slog.Default()),
		),
	})

	assertRoute(t, server, http.MethodGet, "/v2")
	assertRoute(t, server, http.MethodHead, "/v2")
	assertRoute(t, server, http.MethodGet, "/v2/hub/library/alpine/manifests/latest")
	assertRoute(t, server, http.MethodHead, "/v2/hub/library/alpine/blobs/sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	assertRoute(t, server, http.MethodPost, "/v2/hub/library/alpine/blobs/uploads/")
}

func TestGoRootEndpointDoesNotShadowScopedDependencyRoutes(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/left-pad" {
			t.Fatalf("upstream path = %s, want /left-pad", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"name":"left-pad","versions":{}}`)); err != nil {
			t.Fatalf("write upstream response: %v", err)
		}
	}))
	t.Cleanup(upstream.Close)

	metadata, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Fatalf("close metadata: %v", closeErr)
		}
	})
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("open objects: %v", err)
	}

	npmService := npm.NewService(npm.ServiceDependencies{
		Config: config.Config{
			NPM: config.DependencyEcosystemConfig{
				"npmjs": {Registry: upstream.URL},
			},
		},
		Metadata: metadata,
		Objects:  objects,
	})
	baseURL := startAPIServerWithOptions(t, api.Options{
		Endpoints: collectionlist.NewList[httpx.Endpoint](
			golang.NewEndpoint(nil),
			npm.NewEndpoint(npmService),
			golang.NewRootEndpoint(nil),
		),
	})

	resp := httpGet(t, baseURL+"/npm/npmjs/left-pad")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"left-pad"`)) {
		t.Fatalf("body = %q, want npm metadata", body)
	}
}

func assertRoute(t *testing.T, server *api.Server, method, path string) {
	t.Helper()
	if !server.HasRoute(method, path) {
		t.Fatalf("expected route %s %s to match", method, path)
	}
}
