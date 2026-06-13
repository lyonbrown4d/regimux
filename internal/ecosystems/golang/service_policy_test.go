package golang_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/golang"
	"github.com/lyonbrown4d/regimux/internal/policy"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestServiceRootGoProxyDoesNotFallbackOnPolicyDenied(t *testing.T) {
	ctx := context.Background()
	primaryRequests := 0
	primary := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		primaryRequests++
		t.Fatal("primary upstream should not be called when policy denies request")
	}))
	t.Cleanup(primary.Close)

	backupRequests := 0
	backup := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		backupRequests++
		t.Fatal("backup upstream should not be called when policy denies request")
	}))
	t.Cleanup(backup.Close)

	metadata := newTestMetadata(ctx, t)
	service := golang.NewService(golang.ServiceDependencies{
		Config: config.Config{
			Go: map[string]config.DependencyUpstreamConfig{
				"backup":  {Registry: backup.URL},
				"default": {Registry: primary.URL},
			},
			Policy: config.PolicyConfig{
				Dependency: config.DependencyPolicyConfig{
					Block: []config.DependencyRuleConfig{{
						Ecosystem: "go",
						Alias:     "default",
						Artifact:  "github.com/acme/lib",
					}},
				},
			},
		},
		Metadata: metadata,
	})
	_, err := service.Get(ctx, golang.Request{
		Tail: "github.com/acme/lib/@v/v1.2.3.mod",
	})
	if err == nil {
		t.Fatal("expected policy block error")
	}
	if !errors.Is(err, policy.ErrDependencyBlocked) {
		t.Fatalf("unexpected error = %v", err)
	}
	if primaryRequests != 0 {
		t.Fatalf("primary requests = %d, want 0", primaryRequests)
	}
	if backupRequests != 0 {
		t.Fatalf("backup requests = %d, want 0", backupRequests)
	}
	assertPolicyDeniedPull(ctx, t, metadata, meta.PullKey{
		Alias:      "go/default",
		Repository: "github.com/acme/lib",
		Reference:  "@v/v1.2.3.mod",
	})
}

func TestServiceRejectsNonGoUpstream(t *testing.T) {
	ctx := context.Background()
	service := golang.NewService(golang.ServiceDependencies{
		Config: config.Config{
			Container: config.ContainerConfig{
				"hub": {Registry: "https://registry-1.docker.io"},
			},
		},
	})
	_, err := service.Get(ctx, golang.Request{
		Alias: "default",
		Tail:  "github.com/acme/lib/@v/v1.2.3.mod",
	})
	if err == nil {
		t.Fatal("expected non-go upstream error")
	}
}
