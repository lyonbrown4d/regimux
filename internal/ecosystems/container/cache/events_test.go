package cache_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	containercache "github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/events"
)

func TestPublishCacheAccessEmitsLowCardinalityContainerPullEvent(t *testing.T) {
	bus := events.NewBus(slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := bus.Close(); err != nil {
			t.Fatalf("close bus: %v", err)
		}
	})
	received := make(chan events.ContainerPullCacheAccess, 1)
	unsubscribe, err := events.NewSubscriber(func(_ context.Context, event events.ContainerPullCacheAccess) error {
		received <- event
		return nil
	}).Subscribe(bus)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	t.Cleanup(unsubscribe)

	containercache.PublishBlobCacheAccess(context.Background(), bus, containercache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        "sha256:abc",
	}, containercache.CacheHit)

	select {
	case event := <-received:
		if event.Alias != "hub" || event.Kind != "blob" || event.CacheStatus != "hit" {
			t.Fatalf("unexpected container pull cache access event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for container pull cache access event")
	}
}
