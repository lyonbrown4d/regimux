package observability

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestConfiguredUpstreamEndpointsOrdersMirrorsThenPrimary(t *testing.T) {
	t.Parallel()

	got := configuredUpstreamEndpoints(config.UpstreamConfig{
		Mirrors:  []string{"https://mirror-1.example.com/", "  https://mirror-2.example.com"},
		Registry: "https://registry.example.com/",
	})

	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].registry != "https://mirror-1.example.com" || got[0].role != "mirror" {
		t.Fatalf("endpoint0 = %#v, want mirror https://mirror-1.example.com", got[0])
	}
	if got[1].registry != "https://mirror-2.example.com" || got[1].role != "mirror" {
		t.Fatalf("endpoint1 = %#v, want mirror https://mirror-2.example.com", got[1])
	}
	if got[2].registry != "https://registry.example.com" || got[2].role != "primary" {
		t.Fatalf("endpoint2 = %#v, want primary https://registry.example.com", got[2])
	}
}

func TestConfiguredUpstreamEndpointsSkipsBlankValues(t *testing.T) {
	t.Parallel()

	got := configuredUpstreamEndpoints(config.UpstreamConfig{
		Mirrors:  []string{"", "  ", "https://mirror.example.com/"},
		Registry: "   ",
	})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].registry != "https://mirror.example.com" {
		t.Fatalf("endpoint = %#v, want https://mirror.example.com", got[0])
	}
	if got[0].role != "mirror" {
		t.Fatalf("role = %q, want mirror", got[0].role)
	}
}
