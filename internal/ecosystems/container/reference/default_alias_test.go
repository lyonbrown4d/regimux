package reference_test

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
)

type defaultAliasTestCase struct {
	name         string
	path         string
	defaultAlias string
	wantAlias    string
	wantRepo     string
}

func TestParseWithDefaultAlias(t *testing.T) {
	t.Parallel()

	configured := map[string]struct{}{
		"hub":     {},
		"private": {},
	}
	hasAlias := func(alias string) bool {
		_, ok := configured[alias]
		return ok
	}

	for _, tt := range defaultAliasTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			route, err := reference.ParseWithDefaultAlias(tt.path, tt.defaultAlias, hasAlias)
			if err != nil {
				t.Fatalf("ParseWithDefaultAlias() error = %v", err)
			}
			if route.Alias != tt.wantAlias {
				t.Fatalf("alias = %q, want %q", route.Alias, tt.wantAlias)
			}
			if route.Repo != tt.wantRepo {
				t.Fatalf("repo = %q, want %q", route.Repo, tt.wantRepo)
			}
		})
	}
}

func TestParseWithDefaultAliasPreservesPing(t *testing.T) {
	t.Parallel()

	route, err := reference.ParseWithDefaultAlias("/v2/", "hub", func(string) bool { return false })
	if err != nil {
		t.Fatalf("ParseWithDefaultAlias() error = %v", err)
	}
	if route.Kind != reference.RoutePing {
		t.Fatalf("kind = %v, want RoutePing", route.Kind)
	}
}

func defaultAliasTestCases() []defaultAliasTestCase {
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	return []defaultAliasTestCase{
		{
			name:         "namespaced manifest uses default",
			path:         "/v2/library/alpine/manifests/latest",
			defaultAlias: "hub",
			wantAlias:    "hub",
			wantRepo:     "library/alpine",
		},
		{
			name:         "single segment repository uses default",
			path:         "/v2/alpine/manifests/latest",
			defaultAlias: "hub",
			wantAlias:    "hub",
			wantRepo:     "alpine",
		},
		{
			name:         "blob uses default",
			path:         "/v2/library/alpine/blobs/" + digest,
			defaultAlias: "hub",
			wantAlias:    "hub",
			wantRepo:     "library/alpine",
		},
		{
			name:         "tags use default",
			path:         "/v2/library/alpine/tags/list",
			defaultAlias: "hub",
			wantAlias:    "hub",
			wantRepo:     "library/alpine",
		},
		{
			name:         "referrers use default",
			path:         "/v2/library/alpine/referrers/" + digest,
			defaultAlias: "hub",
			wantAlias:    "hub",
			wantRepo:     "library/alpine",
		},
		{
			name:         "configured explicit alias wins",
			path:         "/v2/private/team/app/manifests/latest",
			defaultAlias: "hub",
			wantAlias:    "private",
			wantRepo:     "team/app",
		},
		{
			name:      "empty default preserves legacy parsing",
			path:      "/v2/hub/library/alpine/manifests/latest",
			wantAlias: "hub",
			wantRepo:  "library/alpine",
		},
	}
}
