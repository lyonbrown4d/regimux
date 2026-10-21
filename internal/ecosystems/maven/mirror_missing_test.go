package maven_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

const mirrorMissingPOM = `<project><modelVersion>4.0.0</modelVersion>` +
	`<groupId>com.acme</groupId><artifactId>mirror-demo</artifactId><version>1.0</version></project>`
const mirrorMissingTail = "com/acme/mirror-demo/1.0/mirror-demo-1.0.pom"

type missingMirrorScenario struct {
	name       string
	statuses   []int
	wantStatus int
	wantCalls  int
}

func TestMavenEquivalentMirrorsResolveMissingArtifacts(t *testing.T) {
	tests := []missingMirrorScenario{
		{name: "missing then available", statuses: []int{404, 200}, wantStatus: 200, wantCalls: 2},
		{name: "missing and unavailable then available", statuses: []int{404, 503, 200}, wantStatus: 200, wantCalls: 3},
		{name: "all mirrors missing", statuses: []int{404, 404}, wantStatus: 404, wantCalls: 2},
		{name: "missing then unavailable", statuses: []int{404, 503}, wantStatus: 503, wantCalls: 2},
		{name: "unavailable then missing", statuses: []int{503, 404}, wantStatus: 503, wantCalls: 2},
		{name: "authentication required", statuses: []int{404, 401, 200}, wantStatus: 401, wantCalls: 2},
		{name: "permission denied", statuses: []int{404, 403, 200}, wantStatus: 403, wantCalls: 2},
		{name: "single endpoint missing", statuses: []int{404}, wantStatus: 404, wantCalls: 1},
	}
	for _, test := range tests {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(test.name+"/"+method, func(t *testing.T) {
				runMissingMirrorScenario(t, test, method)
			})
		}
	}
}

func runMissingMirrorScenario(t *testing.T, test missingMirrorScenario, method string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	upstream, calls := newMissingMirrorUpstream(t, method, test.statuses)
	service, metadata := newTestService(ctx, t, map[string]config.DependencyUpstreamConfig{"central": upstream})
	req := maven.Request{Alias: "central", Tail: mirrorMissingTail, Method: method}
	resp, err := service.Get(ctx, req)
	requireNoError(t, "resolve artifact across mirrors", err)
	assertMissingMirrorResponse(t, resp, method, test.wantStatus)
	if method == http.MethodGet && test.wantStatus == http.StatusOK {
		requireStoredTag(ctx, t, metadata, meta.TagKey{
			Alias: "central", Repository: "com/acme/mirror-demo/1.0", Reference: "mirror-demo-1.0.pom",
		})
		cached, cacheErr := service.Get(ctx, req)
		requireNoError(t, "read artifact cached from fallback mirror", cacheErr)
		assertBody(t, cached, mirrorMissingPOM)
		if cached.Cache != cacheHit {
			t.Fatalf("cache = %q, want hit", cached.Cache)
		}
	}
	assertMissingMirrorCalls(t, calls, test.wantCalls)
}

func TestMavenMissingDoesNotClaimSkippedMirrorsAreMissing(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	upstream, calls := newMissingMirrorUpstream(t, http.MethodGet, []int{404, 200})
	service, metadata := newTestService(ctx, t, map[string]config.DependencyUpstreamConfig{"central": upstream})
	now := time.Now().UTC()
	_, err := metadata.UpsertEndpointHealth(ctx, meta.EndpointHealthRecord{
		Alias:         ecosystem.ScopedAlias(ecosystem.Maven, "central"),
		Registry:      upstream.Mirrors[0],
		CooldownUntil: now.Add(time.Hour),
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	requireNoError(t, "mark mirror unavailable", err)
	resp, err := service.Get(ctx, maven.Request{Alias: "central", Tail: mirrorMissingTail})
	if resp != nil {
		closeBody(t, resp.Body)
		t.Fatalf("response status = %d, want upstream error because a mirror was not checked", resp.Status)
	}
	if err == nil {
		t.Fatal("missing response concealed an unchecked mirror")
	}
	assertMissingMirrorCalls(t, calls, 1)
}

func TestMavenMissingAttemptStopsOnCallerCancellation(t *testing.T) {
	started := make(chan struct{})
	var primaryCalls atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryCalls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(primary.Close)

	var mirrorCalls atomic.Int32
	mirror := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mirrorCalls.Add(1)
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(mirror.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service, _ := newTestService(ctx, t, map[string]config.DependencyUpstreamConfig{
		"central": {Registry: primary.URL, Mirrors: []string{mirror.URL}},
	})
	result := make(chan error, 1)
	go func() {
		_, err := service.Get(ctx, maven.Request{Alias: "central", Tail: mirrorMissingTail})
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("fallback mirror did not receive the request")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("Maven lookup succeeded after caller cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Maven lookup did not stop after caller cancellation")
	}
	if got := primaryCalls.Load(); got != 1 {
		t.Fatalf("primary requests = %d, want 1", got)
	}
	if got := mirrorCalls.Load(); got != 1 {
		t.Fatalf("fallback requests = %d, want 1", got)
	}
}

func TestMavenUnavailableFallbackClosesRetainedMissingBody(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		if _, err := io.WriteString(w, "missing artifact"); err != nil {
			t.Errorf("write missing response body: %v", err)
		}
	}))
	t.Cleanup(primary.Close)
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		if _, err := io.WriteString(w, "mirror unavailable"); err != nil {
			t.Errorf("write unavailable response body: %v", err)
		}
	}))
	t.Cleanup(mirror.Close)

	service, _ := newTestService(ctx, t, map[string]config.DependencyUpstreamConfig{
		"central": {Registry: primary.URL, Mirrors: []string{mirror.URL}},
	})
	resp, err := service.Get(ctx, maven.Request{Alias: "central", Tail: mirrorMissingTail})
	requireNoError(t, "resolve unavailable fallback", err)
	if resp.Status != http.StatusServiceUnavailable {
		closeBody(t, resp.Body)
		t.Fatalf("status = %d, want 503", resp.Status)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("read spool directory before closing selected response: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("temporary Maven response files while selected response is open = %d, want only its body", len(entries))
	}
	assertBody(t, resp, "mirror unavailable")
	entries, err = os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("read spool directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary Maven response files remain after closing the selected response: %v", entries)
	}
}

func newMissingMirrorUpstream(
	t *testing.T,
	method string,
	statuses []int,
) (config.DependencyUpstreamConfig, []*atomic.Int32) {
	t.Helper()
	upstream := missingMirrorUpstreamConfig()
	calls := make([]*atomic.Int32, 0, len(statuses))
	for index, status := range statuses {
		counter := &atomic.Int32{}
		calls = append(calls, counter)
		server := httptest.NewServer(missingMirrorHandler(t, counter, method, status))
		t.Cleanup(server.Close)
		if index == 0 {
			upstream.Registry = server.URL
			continue
		}
		upstream.Mirrors = append(upstream.Mirrors, server.URL)
	}
	return upstream, calls
}

func missingMirrorUpstreamConfig() config.DependencyUpstreamConfig {
	return config.DependencyUpstreamConfig{MirrorPolicy: "ordered"}
}

func missingMirrorHandler(t *testing.T, counter *atomic.Int32, method string, status int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		counter.Add(1)
		if !expectedMirrorRequest(t, w, r, method) {
			return
		}
		body := missingMirrorBody(status, w)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(status)
		if method != http.MethodHead {
			writeResponse(t, w, body)
		}
	}
}

func expectedMirrorRequest(t *testing.T, w http.ResponseWriter, r *http.Request, method string) bool {
	t.Helper()
	if r.Method == method && r.URL.Path == "/"+mirrorMissingTail {
		return true
	}
	t.Errorf("unexpected mirror request: %s %s", r.Method, r.URL.Path)
	w.WriteHeader(http.StatusBadRequest)
	return false
}

func missingMirrorBody(status int, w http.ResponseWriter) string {
	if status != http.StatusOK {
		return http.StatusText(status)
	}
	w.Header().Set("Content-Type", "application/xml")
	return mirrorMissingPOM
}

func assertMissingMirrorCalls(t *testing.T, calls []*atomic.Int32, attempted int) {
	t.Helper()
	for index, counter := range calls {
		var want int32
		if index < attempted {
			want = 1
		}
		if got := counter.Load(); got != want {
			t.Errorf("mirror %d calls = %d, want %d", index, got, want)
		}
	}
}

func assertMissingMirrorResponse(t *testing.T, resp *maven.Response, method string, status int) {
	t.Helper()
	body := responseBody(t, resp)
	if resp.Status != status {
		t.Fatalf("status = %d, want %d", resp.Status, status)
	}
	wantBody := http.StatusText(status)
	if status == http.StatusOK {
		wantBody = mirrorMissingPOM
	}
	if resp.Size != int64(len(wantBody)) {
		t.Fatalf("size = %d, want %d", resp.Size, len(wantBody))
	}
	if method == http.MethodHead {
		wantBody = ""
	}
	if body != wantBody {
		t.Fatalf("body = %q, want %q", body, wantBody)
	}
}
