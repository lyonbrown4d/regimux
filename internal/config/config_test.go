package config

import (
	"context"
	"testing"
)

func TestLoadDefaultsIncludeStore(t *testing.T) {
	cfg, err := Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if cfg.Store.Meta.Driver != "bboltx" || cfg.Store.Meta.Path != "data/regimux.db" {
		t.Fatalf("unexpected meta store defaults: %#v", cfg.Store.Meta)
	}
	if cfg.Store.Object.Driver != "local" || cfg.Store.Object.Path != "data/objects" {
		t.Fatalf("unexpected object store defaults: %#v", cfg.Store.Object)
	}
}

func TestValidateStoreRejectsUnsupportedDrivers(t *testing.T) {
	cfg, err := Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	cfg.Store.Meta.Driver = "postgres"
	if err := cfg.NormalizeAndValidate(); err == nil {
		t.Fatal("expected unsupported meta store driver error")
	}

	cfg, err = Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	cfg.Store.Object.Driver = "s3"
	if err := cfg.NormalizeAndValidate(); err == nil {
		t.Fatal("expected unsupported object store driver error")
	}
}
