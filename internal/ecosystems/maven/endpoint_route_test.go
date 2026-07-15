package maven_test

import (
	"net/http"
	"testing"

	"github.com/arcgolabs/httpx"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
)

func TestEndpointRegistersUnifiedMavenRoutes(t *testing.T) {
	server := httpx.New()
	server.RegisterOnly(maven.NewEndpoint(nil))

	const route = "/maven/{alias}/{tail...}"
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if !server.HasRoute(method, route) {
			t.Fatalf("route %s %s is not registered", method, route)
		}
	}

	const legacyRoute = "/maven-group/{alias}/{tail...}"
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if server.HasRoute(method, legacyRoute) {
			t.Fatalf("legacy route %s %s is still registered", method, legacyRoute)
		}
	}
}
