package dockerintegration_test

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/dockerintegration"
)

func TestBuildProxyReference(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		image string
		want  string
	}{
		{
			name:  "official image uses default namespace and latest tag",
			image: "alpine",
			want:  "192.168.1.2:5000/hub/library/alpine:latest",
		},
		{
			name:  "tag is preserved",
			image: "library/nginx:1.27",
			want:  "192.168.1.2:5000/hub/library/nginx:1.27",
		},
		{
			name:  "registry prefix is stripped",
			image: "docker.io/library/redis:7",
			want:  "192.168.1.2:5000/hub/library/redis:7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := dockerintegration.BuildProxyReference(dockerintegration.ProxyReferenceOptions{
				Registry:         "192.168.1.2:5000",
				Alias:            "hub",
				DefaultNamespace: "library",
			}, tt.image)
			if err != nil {
				t.Fatalf("BuildProxyReference() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("BuildProxyReference() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildProxyReferenceRejectsInvalidImage(t *testing.T) {
	t.Parallel()
	_, err := dockerintegration.BuildProxyReference(dockerintegration.ProxyReferenceOptions{
		Registry: "localhost:5000",
		Alias:    "hub",
	}, "bad@@ref")
	if err == nil {
		t.Fatal("expected invalid image reference error")
	}
}
