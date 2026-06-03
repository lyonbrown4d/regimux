package ecosystem_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestEndpointProberPersistsPerEndpointHealth(t *testing.T) {
	ctx := context.Background()
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(healthy.Close)
	unhealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(unhealthy.Close)

	store, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata store", err)
	t.Cleanup(func() { requireNoError(t, "close metadata store", store.Close()) })

	prober := ecosystem.NewEndpointProber(store, nil, slog.New(slog.DiscardHandler))
	err = prober.Probe(ctx, ecosystem.ProbeTarget{
		Ecosystem: ecosystem.NPM,
		Alias:     "default",
		Config: config.UpstreamConfig{
			Registry: healthy.URL,
			Mirrors:  []string{unhealthy.URL},
			Probe: config.UpstreamProbeConfig{
				Enabled:  true,
				Interval: time.Minute,
				Timeout:  time.Second,
				Cooldown: time.Minute,
			},
		},
	})
	requireNoError(t, "probe endpoints", err)

	success := endpointHealth(ctx, t, store, ecosystem.ScopedAlias(ecosystem.NPM, "default"), healthy.URL)
	if success.SuccessCount != 1 || success.FailureCount != 0 || success.LatencySamples != 1 || success.LastSuccessAt.IsZero() {
		t.Fatalf("unexpected success record: %#v", success)
	}
	failure := endpointHealth(ctx, t, store, ecosystem.ScopedAlias(ecosystem.NPM, "default"), unhealthy.URL)
	if failure.FailureCount != 1 || failure.SuccessCount != 0 || failure.ConsecutiveFailures != 1 || failure.CooldownUntil.IsZero() {
		t.Fatalf("unexpected failure record: %#v", failure)
	}
}

func endpointHealth(ctx context.Context, t *testing.T, store meta.Store, alias, registry string) *meta.EndpointHealthRecord {
	t.Helper()
	record, ok, err := store.EndpointHealth(ctx, meta.EndpointHealthKey{
		Alias:    alias,
		Registry: registry,
	})
	requireNoError(t, "get endpoint health", err)
	if !ok {
		t.Fatalf("missing endpoint health alias=%s registry=%s", alias, registry)
	}
	return record
}

func requireNoError(t *testing.T, action string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}
