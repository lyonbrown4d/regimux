package container

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
)

func TestManifestSpoolFailureFinalizesCanonicalMetadataOnly(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	completion := &trackedManifestCompletion{}
	service := spoolFailureManifestService{completion: completion}
	endpoint := NewRegistryEndpoint(service, nil, nil, nil, nil)
	input := &registryInput{}
	route := reference.Route{Alias: "hub", Repo: "library/alpine", Reference: "latest"}

	out := endpoint.metadataResponse(t.Context(), func() (*registryOutput, func() error) {
		return endpoint.manifest(t.Context(), input, route, http.MethodGet)
	})
	if out == nil || out.Status != http.StatusInternalServerError {
		t.Fatalf("spool failure response=%v, want HTTP 500", out)
	}
	if got := completion.finalizes.Load(); got != 1 {
		t.Errorf("canonical finalizations=%d, want 1", got)
	}
	if got := completion.responses.Load(); got != 0 {
		t.Errorf("per-HTTP completions=%d, want 0 after spool failure", got)
	}
}

type spoolFailureManifestService struct {
	completion cache.ManifestCompletion
}

func (s spoolFailureManifestService) Get(context.Context, cache.ManifestRequest) (*cache.CachedManifest, error) {
	return nil, errors.New("unexpected manifest Get call")
}

func (s spoolFailureManifestService) Prepare(context.Context, cache.ManifestRequest) (*cache.CachedManifest, cache.ManifestCompletion, error) {
	return &cache.CachedManifest{Digest: "sha256:abc", MediaType: "application/vnd.oci.image.manifest.v1+json", Size: 1, Body: []byte("x")}, s.completion, nil
}

type trackedManifestCompletion struct {
	finalizes atomic.Int32
	responses atomic.Int32
}

func (c *trackedManifestCompletion) Finalize(context.Context) error {
	c.finalizes.Add(1)
	return nil
}

func (c *trackedManifestCompletion) CompleteResponse(context.Context) error {
	c.responses.Add(1)
	return nil
}
