package suggestion_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/suggestion"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestSuggestManifestMissRefreshesAndCachesTags(t *testing.T) {
	ctx := context.Background()
	client := newFakeTagClient(fakeTagPage{
		body: `{"name":"timescale/timescaledb","tags":["latest-pg17","latest-pg18","latest-pg18-oss","2.20.0-pg18"]}`,
	})
	cache := backend.NewMemory(backend.MemoryOptions{})
	service := suggestion.NewService(suggestion.Dependencies{
		Client: client,
		Cache:  cache,
	})
	req := manifestReq()

	first := service.SuggestManifestMiss(ctx, req)
	if first.Source != suggestion.SourceRefresh || !first.Refreshed {
		t.Fatalf("first source = %s refreshed=%v, want refresh true", first.Source, first.Refreshed)
	}
	if first.RefreshError != nil || first.CacheError != nil {
		t.Fatalf("unexpected errors: refresh=%v cache=%v", first.RefreshError, first.CacheError)
	}
	assertFirstSuggestion(t, first.Suggestions, "latest-pg18")
	if got := client.callCount(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	if got := client.requests()[0].N; got != "1000" {
		t.Fatalf("page size = %q, want 1000", got)
	}

	second := service.SuggestManifestMiss(ctx, req)
	if second.Source != suggestion.SourceCache || second.Refreshed {
		t.Fatalf("second source = %s refreshed=%v, want cache false", second.Source, second.Refreshed)
	}
	assertFirstSuggestion(t, second.Suggestions, "latest-pg18")
	if got := client.callCount(); got != 1 {
		t.Fatalf("calls after cache hit = %d, want 1", got)
	}
}

func TestRecordManifestHitRefreshesTagsAsync(t *testing.T) {
	ctx := context.Background()
	client := newFakeTagClient(fakeTagPage{
		body: `{"tags":["latest-pg18","latest-pg18-oss"]}`,
	})
	cache := backend.NewMemory(backend.MemoryOptions{})
	service := suggestion.NewService(suggestion.Dependencies{
		Client: client,
		Cache:  cache,
		Options: suggestion.Options{
			RefreshTimeout: 200 * time.Millisecond,
		},
	})

	service.RecordManifestHit(ctx, manifestReq())

	reader := suggestion.NewService(suggestion.Dependencies{Cache: cache})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		result := reader.SuggestManifestMiss(ctx, manifestReq())
		if result.Source == suggestion.SourceCache && result.HasSuggestions() {
			assertFirstSuggestion(t, result.Suggestions, "latest-pg18")
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for async tag suggestion refresh")
}

func TestSuggestManifestMissCapturesShortRefreshTimeout(t *testing.T) {
	ctx := context.Background()
	client := &fakeTagClient{blockUntilDone: true}
	service := suggestion.NewService(suggestion.Dependencies{
		Client: client,
		Cache:  backend.NewMemory(backend.MemoryOptions{}),
		Options: suggestion.Options{
			RefreshTimeout: 10 * time.Millisecond,
		},
	})

	result := service.SuggestManifestMiss(ctx, manifestReq())
	if result.Source != suggestion.SourceNone {
		t.Fatalf("source = %s, want none", result.Source)
	}
	if result.HasSuggestions() {
		t.Fatalf("suggestions = %#v, want none", result.Suggestions)
	}
	if result.RefreshError == nil {
		t.Fatal("expected refresh timeout error")
	}
	if got := client.callCount(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestSuggestManifestMissUsesCachedRepositorySuggestions(t *testing.T) {
	ctx := context.Background()
	client := newFakeTagClient(
		fakeTagPage{body: `{"tags":["latest-pg18"]}`},
		fakeTagPage{err: distribution.ErrNameUnknown.WithDetail("missing repository")},
	)
	service := suggestion.NewService(suggestion.Dependencies{
		Client: client,
		Cache:  backend.NewMemory(backend.MemoryOptions{}),
	})

	if _, err := service.RefreshTags(ctx, manifestReq()); err != nil {
		t.Fatalf("prime tags: %v", err)
	}

	result := service.SuggestManifestMiss(ctx, suggestion.ManifestRequest{
		Alias:      "hub",
		Repository: "timescale/timescaled",
		Reference:  "latest-18",
	})
	if result.HasSuggestions() {
		assertStringSlice(t, result.Repositories, []string{"timescale/timescaledb"})
		return
	}
	t.Fatalf("repository suggestions = %#v, want timescale/timescaledb", result.Repositories)
}

func TestRefreshTagsFollowsNextLinkAndDeduplicates(t *testing.T) {
	ctx := context.Background()
	client := newFakeTagClient(
		fakeTagPage{
			body:    `{"tags":["latest-pg17","latest-pg18"]}`,
			headers: http.Header{"Link": {`<https://registry.example/v2/timescale/timescaledb/tags/list?n=2&last=latest-pg18>; rel="next"`}},
		},
		fakeTagPage{
			body: `{"tags":["latest-pg18","latest-pg18-oss"]}`,
		},
	)
	service := suggestion.NewService(suggestion.Dependencies{
		Client: client,
		Cache:  backend.NewMemory(backend.MemoryOptions{}),
		Options: suggestion.Options{
			TagPageSize: 2,
			MaxTagPages: 2,
		},
	})

	index, err := service.RefreshTags(ctx, manifestReq())
	if err != nil {
		t.Fatalf("refresh tags: %v", err)
	}
	want := []string{"latest-pg17", "latest-pg18", "latest-pg18-oss"}
	assertStringSlice(t, index.Tags, want)
	requests := client.requests()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if requests[1].Last != "latest-pg18" {
		t.Fatalf("second last = %q, want latest-pg18", requests[1].Last)
	}
}

func TestSuggestTagsRanksTimescalePostgresTag(t *testing.T) {
	got := suggestion.SuggestTags("latest-18", []string{
		"latest-pg17",
		"latest-pg18",
		"latest-pg18-oss",
		"2.20.0-pg18",
	}, suggestion.SuggestOptions{Limit: 3})
	if len(got) == 0 {
		t.Fatal("expected tag suggestions")
	}
	assertStringSlice(t, got[:1], []string{"latest-pg18"})
}

func manifestReq() suggestion.ManifestRequest {
	return suggestion.ManifestRequest{
		Alias:      "hub",
		Repository: "timescale/timescaledb",
		Reference:  "latest-18",
	}
}

func assertFirstSuggestion(t *testing.T, got []distribution.ManifestSuggestion, want string) {
	t.Helper()
	if len(got) == 0 {
		t.Fatal("expected suggestions")
	}
	if got[0].Reference != want {
		t.Fatalf("first suggestion = %q, want %q", got[0].Reference, want)
	}
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slice len = %d, want %d: got=%#v want=%#v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slice[%d] = %q, want %q: got=%#v want=%#v", i, got[i], want[i], got, want)
		}
	}
}

type fakeTagPage struct {
	body    string
	headers http.Header
	err     error
}

type fakeTagClient struct {
	mu             sync.Mutex
	pages          []fakeTagPage
	calls          []upstream.ListTagsRequest
	blockUntilDone bool
}

func newFakeTagClient(pages ...fakeTagPage) *fakeTagClient {
	return &fakeTagClient{pages: pages}
}

func (c *fakeTagClient) ListTags(ctx context.Context, req upstream.ListTagsRequest) (*upstream.TagsResponse, error) {
	if c.blockUntilDone {
		c.record(req)
		<-ctx.Done()
		return nil, fmt.Errorf("fake tag client context done: %w", ctx.Err())
	}

	page := c.nextPage(req)
	if page.err != nil {
		return nil, page.err
	}
	body := page.body
	if body == "" {
		body = `{"tags":[]}`
	}
	return &upstream.TagsResponse{
		Body:    io.NopCloser(strings.NewReader(body)),
		Headers: page.headers.Clone(),
	}, nil
}

func (c *fakeTagClient) nextPage(req upstream.ListTagsRequest) fakeTagPage {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, req)
	if len(c.pages) == 0 {
		return fakeTagPage{}
	}
	index := len(c.calls) - 1
	if index >= len(c.pages) {
		index = len(c.pages) - 1
	}
	return c.pages[index]
}

func (c *fakeTagClient) record(req upstream.ListTagsRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, req)
}

func (c *fakeTagClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func (c *fakeTagClient) requests() []upstream.ListTagsRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]upstream.ListTagsRequest, len(c.calls))
	copy(out, c.calls)
	return out
}

var (
	_ suggestion.TagClient = (*fakeTagClient)(nil)
)
