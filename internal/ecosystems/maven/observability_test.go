//nolint:testpackage // Event assertions inspect internal event publication helpers.
package maven

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/events"
)

//nolint:cyclop,gocognit,gocyclo // Verify redaction and event fields in one captured event stream.
func TestMavenObservabilityScopesAliasesAndRedactsEndpointSecrets(t *testing.T) {
	bus := events.NewBus(slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := bus.Close(); err != nil {
			t.Errorf("close event bus: %v", err)
		}
	})
	requests := make(chan events.UpstreamRequest, 1)
	failovers := make(chan events.UpstreamFailover, 1)
	cacheAccess := make(chan events.CacheAccess, 1)
	for _, subscriber := range []events.Subscriber{
		events.NewSubscriber(func(_ context.Context, event events.UpstreamRequest) error { requests <- event; return nil }),
		events.NewSubscriber(func(_ context.Context, event events.UpstreamFailover) error { failovers <- event; return nil }),
		events.NewSubscriber(func(_ context.Context, event events.CacheAccess) error { cacheAccess <- event; return nil }),
	} {
		unsubscribe, err := subscriber.Subscribe(bus)
		if err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		t.Cleanup(unsubscribe)
	}

	service := &Service{events: bus}
	route := Route{Alias: "central", Kind: RouteRelease, Repository: "com/acme/demo/1.0", Reference: "demo.jar", UpstreamTail: "com/acme/demo/1.0/demo.jar"}
	service.publishCacheAccess(context.Background(), route, cacheMiss, "")
	service.publishUpstreamRequest(context.Background(), route, "https://user:secret@repo.example.com/private", "GET", 503, 12*time.Millisecond, nil)
	service.publishUpstreamFailover(context.Background(), route, "https://user:secret@repo.example.com/private", "GET", 503, nil, true)

	select {
	case event := <-cacheAccess:
		if event.Alias != "maven/central" || event.Status != cacheMiss || event.Repository != route.Repository {
			t.Fatalf("unexpected cache access event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cache access event")
	}
	select {
	case event := <-requests:
		if event.Alias != "maven/central" || event.Registry != "repo.example.com" || event.Path != route.UpstreamTail {
			t.Fatalf("unexpected upstream request event: %#v", event)
		}
		if event.Error != "http_status_503" || strings.Contains(event.Registry, "secret") || strings.Contains(event.Path, "secret") {
			t.Fatalf("upstream request event leaked endpoint data or lost status: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream request event")
	}
	select {
	case event := <-failovers:
		if event.Alias != "maven/central" || event.Registry != "repo.example.com" || event.Error != "http_status_503" || !event.HasNext {
			t.Fatalf("unexpected upstream failover event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream failover event")
	}
}
