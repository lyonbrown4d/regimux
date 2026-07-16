package maven_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestServiceRejectsInvalidPOMWithoutCaching(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body"},
		{name: "html error body", body: "<html><body>upstream error</body></html>"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			testRejectedPOM(t, test.body)
		})
	}
}

func testRejectedPOM(t *testing.T, rejectedBody string) {
	t.Helper()
	ctx := context.Background()
	var requests atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if requests.Add(1) == 1 {
			writeResponse(t, w, rejectedBody)
			return
		}
		writeResponse(t, w, "<project><modelVersion>4.0.0</modelVersion></project>")
	}))
	t.Cleanup(upstream.Close)

	service, metadata := newTestService(ctx, t, map[string]config.DependencyUpstreamConfig{
		"central": {Registry: upstream.URL},
	})
	request := maven.Request{
		Alias: "central",
		Tail:  "org/example/demo/1.0/demo-1.0.pom",
	}

	first, err := service.Get(ctx, request)
	if err == nil {
		if first != nil && first.Body != nil {
			requireNoError(t, "close rejected POM response", first.Body.Close())
		}
		t.Fatal("expected invalid POM error")
	}
	_, cached, lookupErr := metadata.Tag(ctx, meta.TagKey{
		Alias:      "central",
		Repository: "org/example/demo/1.0",
		Reference:  "demo-1.0.pom",
	})
	requireNoError(t, "lookup rejected POM tag", lookupErr)
	if cached {
		t.Fatal("invalid POM was cached")
	}

	second, err := service.Get(ctx, request)
	requireNoError(t, "second get", err)
	assertBody(t, second, "<project><modelVersion>4.0.0</modelVersion></project>")
	if got := requests.Load(); got != 2 {
		t.Fatalf("upstream requests = %d, want 2", got)
	}
}
