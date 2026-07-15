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

func TestMavenGroupMemberPolicyBlockIsTerminal(t *testing.T) {
	ctx := context.Background()
	internalRequests := 0
	internal := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		internalRequests++
	}))
	t.Cleanup(internal.Close)
	centralRequests := 0
	central := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		centralRequests++
	}))
	t.Cleanup(central.Close)

	metadata := newTestMetadata(ctx, t)
	service := maven.NewService(maven.ServiceDependencies{
		Config: config.Config{
			Maven: config.DependencyEcosystemConfig{
				"internal": {Registry: internal.URL},
				"central":  {Registry: central.URL},
			},
			MavenGroups: config.MavenGroupsConfig{
				"public": {Members: []string{"internal", "central"}},
			},
			Policy: config.PolicyConfig{
				Dependency: config.DependencyPolicyConfig{
					Block: []config.DependencyRuleConfig{{
						Ecosystem: "maven",
						Alias:     "internal",
						Artifact:  "com/acme/demo/1.0",
					}},
				},
			},
		},
		Metadata: metadata,
	})
	_, err := service.GetGroup(ctx, maven.Request{
		Alias: "public",
		Tail:  "com/acme/demo/1.0/demo-1.0.jar",
	})
	if err == nil {
		t.Fatal("expected member policy block")
	}
	if !errors.Is(err, policy.ErrDependencyBlocked) {
		t.Fatalf("unexpected error = %v", err)
	}
	if internalRequests != 0 || centralRequests != 0 {
		t.Fatalf(
			"requests = internal:%d central:%d, want 0 each",
			internalRequests,
			centralRequests,
		)
	}
	assertPolicyDeniedPull(ctx, t, metadata, meta.PullKey{
		Alias:      "maven/internal",
		Repository: "com/acme/demo/1.0",
		Reference:  "demo-1.0.jar",
	})
}

func TestMavenGroupDoesNotFallbackOnUnauthorized(t *testing.T) {
	ctx := context.Background()
	unauthorized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(unauthorized.Close)
	secondRequests := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondRequests++
		writeResponse(t, w, "should not be used")
	}))
	t.Cleanup(second.Close)

	service, _, _ := newTestServiceWithStores(
		ctx,
		t,
		map[string]config.DependencyUpstreamConfig{
			"private": {Registry: unauthorized.URL},
			"central": {Registry: second.URL},
		},
		config.MavenGroupsConfig{
			"public": {
				Members:         []string{"private", "central"},
				FallbackOnError: true,
			},
		},
	)
	response, err := service.GetGroup(ctx, maven.Request{
		Alias: "public",
		Tail:  "com/acme/demo/1.0/demo-1.0.jar",
	})
	requireNoError(t, "unauthorized group get", err)
	if response.Status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Status)
	}
	closeResponse(t, response)
	if secondRequests != 0 {
		t.Fatalf("second member requests = %d, want 0", secondRequests)
	}
}
