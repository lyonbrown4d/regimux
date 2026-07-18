package containerauth_test

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	containerauth "github.com/lyonbrown4d/regimux/internal/ecosystems/container/auth"
)

func TestResourceResolverUsesDefaultContainerAlias(t *testing.T) {
	t.Parallel()

	resolver := containerauth.NewResourceResolver(config.Config{
		DefaultContainerAlias: "hub",
		Container: config.ContainerConfig{
			"hub":     {DefaultNamespace: "library"},
			"private": {DefaultNamespace: "team"},
		},
	})

	tests := []struct {
		name         string
		path         string
		wantResource string
	}{
		{
			name:         "standard namespaced repository",
			path:         "/v2/library/alpine/manifests/latest",
			wantResource: "hub/library/alpine",
		},
		{
			name:         "standard single segment repository",
			path:         "/v2/alpine/manifests/latest",
			wantResource: "hub/library/alpine",
		},
		{
			name:         "configured explicit alias wins",
			path:         "/v2/private/tool/manifests/latest",
			wantResource: "private/team/tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resource, matched, err := resolver.ResolvePath(tt.path)
			if err != nil {
				t.Fatalf("ResolvePath() error = %v", err)
			}
			if !matched {
				t.Fatal("ResolvePath() did not match container route")
			}
			if resource.Resource != tt.wantResource {
				t.Fatalf("resource = %q, want %q", resource.Resource, tt.wantResource)
			}
		})
	}
}
