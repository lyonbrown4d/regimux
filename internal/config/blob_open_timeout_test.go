package config_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestLoadBlobOpenTimeout(t *testing.T) {
	for _, test := range []struct {
		name    string
		hcl     string
		env     string
		want    time.Duration
		wantErr bool
	}{
		{name: "default", want: 2 * time.Second},
		{name: "HCL explicit", hcl: "3s", want: 3 * time.Second},
		{name: "HCL zero uses default", hcl: "0s", want: 2 * time.Second},
		{name: "HCL negative rejected", hcl: "-1s", wantErr: true},
		{name: "environment overrides HCL", hcl: "5s", env: "3s", want: 3 * time.Second},
		{name: "environment zero uses default", env: "0s", want: 2 * time.Second},
		{name: "environment negative rejected", env: "-1s", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := loadBlobOpenTimeoutConfig(t, test.hcl, test.env)
			assertBlobOpenTimeoutLoadResult(t, cfg, err, test.want, test.wantErr)
		})
	}
}

func loadBlobOpenTimeoutConfig(t *testing.T, hcl, env string) (config.Config, error) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	unsetEnv(t, "REGIMUX_CACHE__BLOB__OPEN_TIMEOUT")
	attribute := ""
	if hcl != "" {
		attribute = fmt.Sprintf("    open_timeout = %q\n", hcl)
	}
	writeTestConfigFile(t, dir, "regimux.hcl", "cache {\n  blob {\n"+attribute+"  }\n}\n")
	if env != "" {
		t.Setenv("REGIMUX_CACHE__BLOB__OPEN_TIMEOUT", env)
	}
	cfg, err := config.Load(t.Context(), filepath.Join(dir, "regimux.hcl"))
	if err != nil {
		return cfg, fmt.Errorf("load blob open timeout fixture: %w", err)
	}
	return cfg, nil
}

func assertBlobOpenTimeoutLoadResult(t *testing.T, cfg config.Config, err error, want time.Duration, wantErr bool) {
	t.Helper()
	if wantErr {
		if err == nil {
			t.Fatal("negative cache.blob.open_timeout must fail configuration validation")
		}
		return
	}
	if err != nil {
		t.Fatalf("load blob open timeout: %v", err)
	}
	assertPublicBlobOpenTimeout(t, cfg, want)
}

func assertPublicBlobOpenTimeout(t *testing.T, cfg config.Config, want time.Duration) {
	t.Helper()
	data, err := json.Marshal(cfg.Cache.Blob)
	if err != nil {
		t.Fatalf("marshal public blob config: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("decode public blob config: %v", err)
	}
	raw, ok := fields["open_timeout"]
	if !ok {
		t.Fatal("public cache.blob config is missing open_timeout")
	}
	var got time.Duration
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode blob open_timeout duration: %v", err)
	}
	if got != want {
		t.Fatalf("cache.blob.open_timeout = %s, want %s", got, want)
	}
}
