package maven_test

import (
	"net/http"
	"testing"

	"github.com/arcgolabs/httpx"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
)

func TestEndpointRegistersSeparatePhysicalAndGroupRoutes(t *testing.T) {
	server := httpx.New()
	server.RegisterOnly(maven.NewEndpoint(nil))

	routes := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/maven/{alias}/{tail...}"},
		{method: http.MethodHead, path: "/maven/{alias}/{tail...}"},
		{method: http.MethodGet, path: "/maven-group/{alias}/{tail...}"},
		{method: http.MethodHead, path: "/maven-group/{alias}/{tail...}"},
	}
	for _, route := range routes {
		if !server.HasRoute(route.method, route.path) {
			t.Fatalf("route %s %s is not registered", route.method, route.path)
		}
	}
}
