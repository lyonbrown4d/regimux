package maven_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

const resolvedUpstreamHeaderName = "X-Regimux-Upstream"

func TestMavenGroupFallsThroughMissAndCachesResolvedMember(t *testing.T) {
	ctx := context.Background()
	internalRequests := 0
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		internalRequests++
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(internal.Close)

	centralRequests := 0
	central := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		centralRequests++
		w.Header().Set("Content-Type", "application/java-archive")
		writeResponse(t, w, "group jar")
	}))
	t.Cleanup(central.Close)

	service, metadata, _ := newTestServiceWithStores(
		ctx,
		t,
		map[string]config.DependencyUpstreamConfig{
			"internal": {Registry: internal.URL},
			"central":  {Registry: central.URL},
		},
		config.MavenGroupsConfig{
			"public": {Members: []string{"internal", "central"}},
		},
	)
	request := maven.Request{
		Alias: "public",
		Tail:  "com/acme/demo/1.0/demo-1.0.jar",
	}

	if _, err := service.Get(ctx, request); err == nil {
		t.Fatal("physical Maven route unexpectedly resolved a Maven group")
	}
	first, err := service.GetGroup(ctx, request)
	requireNoError(t, "first group get", err)
	if first.Headers.Get(resolvedUpstreamHeaderName) != "central" {
		t.Fatalf(
			"resolved upstream = %q, want central",
			first.Headers.Get(resolvedUpstreamHeaderName),
		)
	}
	assertBody(t, first, "group jar")

	second, err := service.GetGroup(ctx, request)
	requireNoError(t, "cached group get", err)
	if second.Cache != cacheHit {
		t.Fatalf("cache = %q, want hit", second.Cache)
	}
	if second.Headers.Get(resolvedUpstreamHeaderName) != "central" {
		t.Fatalf(
			"cached resolved upstream = %q, want central",
			second.Headers.Get(resolvedUpstreamHeaderName),
		)
	}
	assertBody(t, second, "group jar")

	if internalRequests != 1 || centralRequests != 1 {
		t.Fatalf(
			"requests = internal:%d central:%d, want 1 each",
			internalRequests,
			centralRequests,
		)
	}
	if _, ok, err := metadata.Tag(ctx, meta.TagKey{
		Alias:      "public",
		Repository: "com/acme/demo/1.0",
		Reference:  "demo-1.0.jar",
	}); err != nil || !ok {
		t.Fatalf("group cache metadata: ok=%t error=%v", ok, err)
	}
}

func TestMavenGroupStopsOnServerErrorByDefault(t *testing.T) {
	ctx := context.Background()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(first.Close)

	secondRequests := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondRequests++
		writeResponse(t, w, "should not be used")
	}))
	t.Cleanup(second.Close)

	service, _, _ := newTestServiceWithStores(
		ctx,
		t,
		map[string]config.DependencyUpstreamConfig{
			"first":  {Registry: first.URL},
			"second": {Registry: second.URL},
		},
		config.MavenGroupsConfig{
			"public": {Members: []string{"first", "second"}},
		},
	)
	response, err := service.GetGroup(ctx, maven.Request{
		Alias: "public",
		Tail:  "com/acme/demo/1.0/demo-1.0.jar",
	})
	requireNoError(t, "group get", err)
	if response.Status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Status)
	}
	closeResponse(t, response)
	if secondRequests != 0 {
		t.Fatalf("second member requests = %d, want 0", secondRequests)
	}
}

func TestMavenGroupCanFallbackOnServerError(t *testing.T) {
	ctx := context.Background()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(first.Close)

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeResponse(t, w, "fallback jar")
	}))
	t.Cleanup(second.Close)

	service, _, _ := newTestServiceWithStores(
		ctx,
		t,
		map[string]config.DependencyUpstreamConfig{
			"first":  {Registry: first.URL},
			"second": {Registry: second.URL},
		},
		config.MavenGroupsConfig{
			"public": {
				Members:         []string{"first", "second"},
				FallbackOnError: true,
			},
		},
	)
	response, err := service.GetGroup(ctx, maven.Request{
		Alias: "public",
		Tail:  "com/acme/demo/1.0/demo-1.0.jar",
	})
	requireNoError(t, "fallback group get", err)
	if response.Headers.Get(resolvedUpstreamHeaderName) != "second" {
		t.Fatalf(
			"resolved upstream = %q, want second",
			response.Headers.Get(resolvedUpstreamHeaderName),
		)
	}
	assertBody(t, response, "fallback jar")
}

func TestMavenGroupKeepsSnapshotMetadataFirstHit(t *testing.T) {
	ctx := context.Background()
	first := metadataServer(t, "<metadata><version>1.0-SNAPSHOT</version></metadata>")
	secondRequests := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondRequests++
		writeResponse(t, w, "<metadata><version>wrong</version></metadata>")
	}))
	t.Cleanup(first.Close)
	t.Cleanup(second.Close)

	service, _, _ := newTestServiceWithStores(
		ctx,
		t,
		map[string]config.DependencyUpstreamConfig{
			"internal": {Registry: first.URL},
			"central":  {Registry: second.URL},
		},
		config.MavenGroupsConfig{
			"public": {Members: []string{"internal", "central"}},
		},
	)
	response, err := service.GetGroup(ctx, maven.Request{
		Alias: "public",
		Tail:  "com/acme/demo/1.0-SNAPSHOT/maven-metadata.xml",
	})
	requireNoError(t, "snapshot metadata get", err)
	assertBody(t, response, "<metadata><version>1.0-SNAPSHOT</version></metadata>")
	if secondRequests != 0 {
		t.Fatalf("second member requests = %d, want 0", secondRequests)
	}
}

func TestMavenTargetsIncludeGroupsWithoutChangingPhysicalUpstreams(t *testing.T) {
	ctx := context.Background()
	upstream := metadataServer(t, "<metadata/>")
	t.Cleanup(upstream.Close)
	service, _, _ := newTestServiceWithStores(
		ctx,
		t,
		map[string]config.DependencyUpstreamConfig{
			"central": {Registry: upstream.URL},
		},
		config.MavenGroupsConfig{
			"public": {Members: []string{"central"}},
		},
	)

	if len(service.Upstreams().Values()) != 1 {
		t.Fatalf("physical upstreams = %d, want 1", len(service.Upstreams().Values()))
	}
	targets := service.Targets().Values()
	aliases := make([]string, 0, len(targets))
	for _, target := range targets {
		aliases = append(aliases, target.Alias)
	}
	if !slices.Equal(aliases, []string{"central", "public"}) {
		t.Fatalf("target aliases = %v, want [central public]", aliases)
	}
}

func metadataServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		writeResponse(t, w, body)
	}))
}

func closeResponse(t *testing.T, response *maven.Response) {
	t.Helper()
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
}
