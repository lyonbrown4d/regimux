package upstream

import (
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
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
		},
		Probe: config.UpstreamProbeConfig{
			Enabled:  true,
			Interval: 30 * time.Second,
			Timeout:  3 * time.Second,
			Cooldown: 2 * time.Minute,
		},
	}

	got := toUpstreamConfig("hub", cfg)
	if got.Blob.MirrorPolicy != "latency" || got.Blob.TopN != 3 || got.Blob.MaxConcurrencyPerEndpoint != 4 {
		t.Fatalf("unexpected blob mapping: %#v", got.Blob)
	}
	if !got.Probe.Enabled || got.Probe.Interval != 30*time.Second || got.Probe.Timeout != 3*time.Second || got.Probe.Cooldown != 2*time.Minute {
		t.Fatalf("unexpected probe mapping: %#v", got.Probe)
	}
}
