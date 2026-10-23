package upstream_test

import (
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
)

func TestToUpstreamConfigMapsBlobAndProbe(t *testing.T) {
	cfg := config.UpstreamConfig{
		Registry:     "https://registry.example.com",
		Mirrors:      []string{"https://mirror.example.com"},
		MirrorPolicy: "ordered",
		TagTTL:       10 * time.Minute,
		Blob: config.UpstreamBlobConfig{
			MirrorPolicy:              "latency",
			TopN:                      3,
			MaxConcurrencyPerEndpoint: 4,
			MaxConcurrentAttempts:     2,
		},
		Probe: config.UpstreamProbeConfig{
			Enabled:  true,
			Interval: 30 * time.Second,
			Timeout:  3 * time.Second,
			Cooldown: 2 * time.Minute,
		},
		HTTP: config.HTTPConfig{
			AttemptTimeout: 100 * time.Millisecond,
			HTTP2:          config.HTTP2Config{Enabled: true},
		},
	}

	got := upstream.ConfigFromUpstreamConfig("hub", cfg)
	assertUpstreamBlobConfig(t, got.Blob)
	assertUpstreamProbeConfig(t, got.Probe)
	assertUpstreamHTTPConfig(t, got.HTTP)
}

func assertUpstreamBlobConfig(t *testing.T, got upstream.BlobConfig) {
	t.Helper()
	if got.MirrorPolicy != "latency" || got.TopN != 3 || got.MaxConcurrencyPerEndpoint != 4 || got.MaxConcurrentAttempts != 2 {
		t.Fatalf("unexpected blob mapping: %#v", got)
	}
}

func assertUpstreamProbeConfig(t *testing.T, got upstream.ProbeConfig) {
	t.Helper()
	if !got.Enabled || got.Interval != 30*time.Second || got.Timeout != 3*time.Second || got.Cooldown != 2*time.Minute {
		t.Fatalf("unexpected probe mapping: %#v", got)
	}
}

func assertUpstreamHTTPConfig(t *testing.T, got upstream.HTTPConfig) {
	t.Helper()
	if !got.HTTP2.Enabled {
		t.Fatalf("unexpected http2 mapping: %#v", got.HTTP2)
	}
	if got.AttemptTimeout != 100*time.Millisecond {
		t.Fatalf("unexpected header attempt timeout mapping: %s", got.AttemptTimeout)
	}
}
