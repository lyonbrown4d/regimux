package pypi_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestSimpleIndexPullCompletesCanonicalAndCatalogWrites(t *testing.T) {
	for _, existingCatalog := range []bool{false, true} {
		name := "missing catalog"
		if existingCatalog {
			name = "existing catalog"
		}
		t.Run(name, func(t *testing.T) { assertSimplePullWriteCompleted(t, existingCatalog) })
	}
}

func assertSimplePullWriteCompleted(t *testing.T, existingCatalog bool) {
	t.Helper()
	const body = "<html><body>demo</body></html>"
	upstream, _ := newSimpleBudgetUpstream(t, body)
	hook := &slowSimplePullCatalog{entered: make(chan struct{})}
	metadata := newSimplePullBudgetStore(t, hook)
	key := meta.PullKey{Alias: "pypi/pypi", Repository: "pypi/simple/demo", Reference: "index.html"}
	if existingCatalog {
		_, err := metadata.UpsertManifest(t.Context(), meta.ManifestRecord{
			Alias: key.Alias, Repository: key.Repository, Digest: "sha256:" + strings.Repeat("a", 64),
			Reference: "seed", MediaType: "text/html",
		})
		requireNoError(t, "seed empty repository catalog", err)
	}
	hook.armed.Store(true)
	objects, err := object.NewLocal(t.TempDir())
	requireNoError(t, "open pull test objects", err)
	service := pypi.NewService(pypi.ServiceDependencies{
		Config:   config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: upstream.URL}}},
		Metadata: metadata, Objects: objects,
	})
	client, proxyURL := startStdProxy(t, service)
	client.Timeout = 3 * time.Second
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, proxyURL+"/pypi/pypi/simple/demo/", http.NoBody)
	requireNoError(t, "create simple index request", err)
	done := make(chan simplePullHTTPResult, 1)
	go func() { done <- fetchSimplePullHTTP(client, request) }()
	select {
	case <-hook.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("pull recording never reached the delayed catalog query")
	}
	assertSimplePullCommitted(t, metadata, key)
	result := <-done
	requireNoError(t, "fetch complete simple index", result.err)
	if result.status != http.StatusOK || result.cache != cacheMiss || string(result.body) != body {
		t.Fatalf("simple response status=%d cache=%s body=%q", result.status, result.cache, result.body)
	}
	assertSimplePullCatalog(t, metadata, key)
	if !hook.completed.Load() || hook.canceled.Load() {
		t.Errorf("finite catalog work did not finish: completed=%t canceled=%t", hook.completed.Load(), hook.canceled.Load())
	}
}

type simplePullHTTPResult struct {
	status int
	cache  string
	body   []byte
	err    error
}

func fetchSimplePullHTTP(client *http.Client, request *http.Request) (result simplePullHTTPResult) {
	response, err := client.Do(request)
	if err != nil {
		return simplePullHTTPResult{err: fmt.Errorf("fetch simple index: %w", err)}
	}
	defer func() { result.err = errors.Join(result.err, response.Body.Close()) }()
	result.status, result.cache = response.StatusCode, response.Header.Get("X-Mirror-Cache")
	result.body, err = io.ReadAll(response.Body)
	if err != nil {
		result.err = fmt.Errorf("read simple index: %w", err)
	}
	return result
}

type slowSimplePullCatalog struct {
	armed     atomic.Bool
	claimed   atomic.Bool
	completed atomic.Bool
	canceled  atomic.Bool
	entered   chan struct{}
}

func (h *slowSimplePullCatalog) before(ctx context.Context, event *dbx.HookEvent) (context.Context, error) {
	if !h.armed.Load() || !isSimplePullCatalogRead(event) || !h.claimed.CompareAndSwap(false, true) {
		return ctx, nil
	}
	close(h.entered)
	timer := time.NewTimer(2100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		h.completed.Store(true)
		return ctx, nil
	case <-ctx.Done():
		h.canceled.Store(true)
		return ctx, fmt.Errorf("complete finite catalog query: %w", ctx.Err())
	}
}

func isSimplePullCatalogRead(event *dbx.HookEvent) bool {
	if event.Args == nil || !strings.Contains(strings.ToLower(event.SQL), "meta_upstreams") {
		return false
	}
	for _, arg := range event.Args.Values() {
		if alias, ok := arg.(string); ok && alias == "pypi/pypi" {
			return true
		}
	}
	return false
}

func newSimplePullBudgetStore(t *testing.T, hook *slowSimplePullCatalog) meta.Store {
	t.Helper()
	metadata, err := meta.OpenSQLiteWithOptions(t.Context(), meta.DBOptions{
		Path: filepath.Join(t.TempDir(), "pull-budget.db"), Hooks: []dbx.Hook{dbx.HookFuncs{BeforeFunc: hook.before}},
	})
	requireNoError(t, "open pull budget metadata", err)
	t.Cleanup(func() { requireNoError(t, "close pull budget metadata", metadata.Close()) })
	return metadata
}

func assertSimplePullCommitted(t *testing.T, metadata meta.Store, key meta.PullKey) {
	t.Helper()
	record, found, err := metadata.Pull(t.Context(), key)
	if err != nil || !found || record.Count != 1 {
		t.Fatalf("canonical pull was not committed before catalog refresh: record=%#v found=%t error=%v", record, found, err)
	}
}

func assertSimplePullCatalog(t *testing.T, metadata meta.Store, key meta.PullKey) {
	t.Helper()
	assertSimplePullCommitted(t, metadata, key)
	pull, _, err := metadata.Pull(t.Context(), key)
	requireNoError(t, "read completed pull", err)
	if pull.LastUpstreamPullAt.IsZero() {
		t.Error("successful cold index did not finish its upstream-pull record")
	}
	upstream, err := metadata.UpstreamByAlias(t.Context(), key.Alias)
	if err != nil {
		t.Errorf("successful HTTP pull left its canonical catalog missing: %v", err)
		return
	}
	repository, err := metadata.RepositoryByName(t.Context(), upstream.ID, key.Repository)
	if err != nil || repository.PullCount != pull.Count {
		t.Errorf("canonical/catalog pull counts disagree: canonical=%d repository=%#v error=%v", pull.Count, repository, err)
	}
}
