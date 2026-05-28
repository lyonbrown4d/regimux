package config_test

import (
	"testing"
)

func TestValidateStoreRejectsUnsupportedDrivers(t *testing.T) {
	cfg := loadDefaultConfig(t)
	cfg.Store.Meta.Driver = "unknown"
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr == nil {
		t.Fatal("expected unsupported meta store driver error")
	}

	cfg = loadDefaultConfig(t)
	cfg.Store.Object.Driver = "s3"
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr == nil {
		t.Fatal("expected unsupported object store driver error")
	}
}

func TestValidateStoreAcceptsExternalMetaDrivers(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres", "pg", "postgresql"} {
		assertExternalMetaDriverAccepted(t, driver)
	}
}

func assertExternalMetaDriverAccepted(t *testing.T, driver string) {
	t.Helper()

	cfg := loadDefaultConfig(t)
	cfg.Store.Meta.Driver = driver
	cfg.Store.Meta.DSN = "metadata-dsn"
	cfg.Store.Meta.Path = ""
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr != nil {
		t.Fatalf("normalize external meta store: %v", normalizeErr)
	}
	if cfg.Store.Meta.Driver != "mysql" && cfg.Store.Meta.Driver != "postgres" {
		t.Fatalf("unexpected normalized meta driver: %#v", cfg.Store.Meta)
	}
}

func TestValidateStoreRejectsExternalMetaWithoutDSN(t *testing.T) {
	cfg := loadDefaultConfig(t)
	cfg.Store.Meta.Driver = "postgres"
	cfg.Store.Meta.DSN = ""
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr == nil {
		t.Fatal("expected external meta dsn validation error")
	}
}

func TestValidateStoreAcceptsMemoryObjectDriver(t *testing.T) {
	cfg := loadDefaultConfig(t)
	cfg.Store.Object.Driver = "MEMORY"
	cfg.Store.Object.Path = ""
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr != nil {
		t.Fatalf("normalize memory object store: %v", normalizeErr)
	}
	if cfg.Store.Object.Driver != "memory" || cfg.Store.Object.Path != "data/objects" {
		t.Fatalf("unexpected object store config: %#v", cfg.Store.Object)
	}
}
