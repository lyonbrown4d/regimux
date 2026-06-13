package maven_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
	"github.com/lyonbrown4d/regimux/internal/policy"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestServiceBlockedByPolicyDoesNotFetchUpstream(t *testing.T) {
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		requests++
		t.Fatal("upstream should not be called when policy blocks maven request")
	}))
	t.Cleanup(upstream.Close)

	metadata := newTestMetadata(ctx, t)
	service := maven.NewService(maven.ServiceDependencies{
		Config: config.Config{
			Maven: config.DependencyEcosystemConfig{
				"central": {Registry: upstream.URL},
			},
			Policy: config.PolicyConfig{
				Dependency: config.DependencyPolicyConfig{
					Block: []config.DependencyRuleConfig{{
						Ecosystem: "maven",
						Alias:     "central",
						Artifact:  "com/acme/demo/1.2.3",
					}},
				},
			},
		},
		Metadata: metadata,
	})
	_, err := service.Get(ctx, maven.Request{
		Alias: "central",
		Tail:  "com/acme/demo/1.2.3/demo-1.2.3.jar",
	})
	if err == nil {
		t.Fatal("expected policy block error")
	}
	if !errors.Is(err, policy.ErrDependencyBlocked) {
		t.Fatalf("unexpected error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("upstream requests = %d, want 0", requests)
	}
	assertPolicyDeniedPull(ctx, t, metadata, meta.PullKey{
		Alias:      "maven/central",
		Repository: "com/acme/demo/1.2.3",
		Reference:  "demo-1.2.3.jar",
	})
}

func TestServiceRejectsNonMavenUpstream(t *testing.T) {
	ctx := context.Background()
	service := maven.NewService(maven.ServiceDependencies{
		Config: config.Config{
			Go: config.DependencyEcosystemConfig{
				"default": {Registry: "https://proxy.golang.org"},
			},
		},
	})
	_, err := service.Get(ctx, maven.Request{
		Alias: "default",
		Tail:  "com/acme/demo/1.2.3/demo-1.2.3.jar",
	})
	if err == nil {
		t.Fatal("expected non-maven upstream error")
	}
}
