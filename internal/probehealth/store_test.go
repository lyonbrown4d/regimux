package probehealth_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/probehealth"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestMemoryBackendUsesSafeNoopStore(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Cache.Backend = "memory"

	store := probehealth.NewStore(cfg, slog.New(slog.DiscardHandler))
	if store == nil {
		t.Fatal("store is nil")
	}

	ctx := context.Background()
	err := store.Put(ctx, meta.EndpointHealthRecord{
		Alias:    "hub",
		Registry: "https://registry-1.docker.io",
	})
	if err != nil {
		t.Fatalf("put noop health: %v", err)
	}

	records, err := store.List(ctx, "hub")
	if err != nil {
		t.Fatalf("list noop health: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %#v, want empty", records)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close noop store: %v", err)
	}
}
