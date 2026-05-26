// Package reference_test verifies reference helpers through exported APIs.
package reference_test

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/reference"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestParsePathPing(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/v2", "/v2/"} {
		got, err := reference.ParsePath(path)
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v", path, err)
		}
		if got.Kind != reference.RoutePing {
			t.Fatalf("ParsePath(%q).Kind = %s, want %s", path, got.Kind, reference.RoutePing)
		}
	}
}

func TestParsePathManifest(t *testing.T) {
	t.Parallel()

	got, err := reference.ParsePath("/v2/hub/library/nginx/manifests/latest")
	if err != nil {
		t.Fatalf("ParsePath manifest error = %v", err)
	}
	if got.Kind != reference.RouteManifest || got.Alias != "hub" || got.Repo != "library/nginx" || got.Reference != "latest" {
		t.Fatalf("ParsePath manifest = %+v", got)
	}
	if got.MirrorRepo() != "hub/library/nginx" {
		t.Fatalf("MirrorRepo() = %q", got.MirrorRepo())
	}
}

func TestRouteWithDefaultNamespace(t *testing.T) {
	t.Parallel()

	official := reference.Route{Kind: reference.RouteManifest, Alias: "hub", Repo: "hello-world", Reference: "latest"}.
		WithDefaultNamespace("library")
	if official.Repo != "library/hello-world" {
		t.Fatalf("official repo = %q, want library/hello-world", official.Repo)
	}

	nested := reference.Route{Kind: reference.RouteManifest, Alias: "hub", Repo: "library/hello-world", Reference: "latest"}.
		WithDefaultNamespace("library")
	if nested.Repo != "library/hello-world" {
		t.Fatalf("nested repo = %q, want library/hello-world", nested.Repo)
	}
}

func TestParsePathManifestDigestReference(t *testing.T) {
	t.Parallel()

	got, err := reference.ParseManifestPath("/v2/hub/library/nginx/manifests/SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if err != nil {
		t.Fatalf("ParseManifestPath digest error = %v", err)
	}
	if got.Reference != testDigest {
		t.Fatalf("Reference = %q, want %q", got.Reference, testDigest)
	}
}

func TestParsePathBlob(t *testing.T) {
	t.Parallel()

	got, err := reference.ParsePath("/v2/quay/coreos/etcd/blobs/" + testDigest)
	if err != nil {
		t.Fatalf("ParsePath blob error = %v", err)
	}
	if got.Kind != reference.RouteBlob || got.Alias != "quay" || got.Repo != "coreos/etcd" || got.Digest != testDigest {
		t.Fatalf("ParsePath blob = %+v", got)
	}
}

func TestParsePathTags(t *testing.T) {
	t.Parallel()

	got, err := reference.ParsePath("/v2/ghcr/org/app/tags/list")
	if err != nil {
		t.Fatalf("ParsePath tags error = %v", err)
	}
	if got.Kind != reference.RouteTags || got.Alias != "ghcr" || got.Repo != "org/app" {
		t.Fatalf("ParsePath tags = %+v", got)
	}
}

func TestParsePathReferrers(t *testing.T) {
	t.Parallel()

	got, err := reference.ParsePath("/v2/ghcr/org/app/referrers/" + testDigest)
	if err != nil {
		t.Fatalf("ParsePath referrers error = %v", err)
	}
	if got.Kind != reference.RouteReferrers || got.Alias != "ghcr" || got.Repo != "org/app" || got.Digest != testDigest {
		t.Fatalf("ParsePath referrers = %+v", got)
	}
}

func TestParsePathUsesLastOperationMarker(t *testing.T) {
	t.Parallel()

	got, err := reference.ParsePath("/v2/hub/team/manifests/app/manifests/latest")
	if err != nil {
		t.Fatalf("ParsePath with marker in repo error = %v", err)
	}
	if got.Repo != "team/manifests/app" || got.Reference != "latest" {
		t.Fatalf("ParsePath with marker in repo = %+v", got)
	}
}

func TestParsePathRejectsInvalid(t *testing.T) {
	t.Parallel()

	tests := []string{
		"/v1/hub/library/nginx/manifests/latest",
		"/v2/hub/manifests/latest",
		"/v2/hub/library//nginx/manifests/latest",
		"/v2/hub/localhost:5000/nginx/manifests/latest",
		"/v2/hub/library/nginx/blobs/not-a-digest",
		"/v2/hub/library/nginx/tags",
		"/v2/hub/library/nginx/referrers/not-a-digest",
	}
	for _, tt := range tests {
		if _, err := reference.ParsePath(tt); err == nil {
			t.Fatalf("ParsePath(%q) expected error", tt)
		}
	}
}
