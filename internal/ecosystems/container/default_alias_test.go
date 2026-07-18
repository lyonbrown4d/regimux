package container_test

import (
	"net/http"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container"
)

func TestRegistryEndpointUsesDefaultContainerAlias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		wantRepo string
	}{
		{
			name:     "standard namespaced repository",
			path:     "/v2/library/alpine/manifests/latest",
			wantRepo: "library/alpine",
		},
		{
			name:     "standard single segment repository",
			path:     "/v2/alpine/manifests/latest",
			wantRepo: "library/alpine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			manifests := &recordingManifestService{}
			endpoint := container.NewRegistryEndpointFromConfig(
				manifests,
				nil,
				nil,
				nil,
				nil,
				config.Config{
					DefaultContainerAlias: "hub",
					Container: config.ContainerConfig{
						"hub": {DefaultNamespace: "library"},
					},
				},
			)
			baseURL := startAPIServer(t, endpoint)

			resp := httpGet(t, baseURL+tt.path)
			body := readHTTPResponse(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
			}
			if manifests.Repo() != tt.wantRepo {
				t.Fatalf("repo = %q, want %q", manifests.Repo(), tt.wantRepo)
			}
		})
	}
}

func TestRegistryEndpointConfiguredAliasTakesPrecedenceOverDefault(t *testing.T) {
	t.Parallel()

	manifests := &recordingManifestService{}
	endpoint := container.NewRegistryEndpointFromConfig(
		manifests,
		nil,
		nil,
		nil,
		nil,
		config.Config{
			DefaultContainerAlias: "hub",
			Container: config.ContainerConfig{
				"hub":     {DefaultNamespace: "library"},
				"private": {DefaultNamespace: "team"},
			},
		},
	)
	baseURL := startAPIServer(t, endpoint)

	resp := httpGet(t, baseURL+"/v2/private/tool/manifests/latest")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}
	if manifests.Repo() != "team/tool" {
		t.Fatalf("repo = %q, want team/tool", manifests.Repo())
	}
}
