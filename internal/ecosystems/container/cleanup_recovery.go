package container

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

// Complete the graph before planning any deletions. Never infer an empty graph
// from a missing body: even an unavailable index may own shared layers.
func (r *Runtime) recoverCleanupManifests(ctx context.Context, manifests []meta.ManifestRecord, dryRun bool) error {
	for i := range manifests {
		manifest := manifests[i]
		if !isContainerManifest(manifest) {
			continue
		}
		if err := r.ensureCleanupManifest(ctx, manifest, dryRun); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) ensureCleanupManifest(ctx context.Context, manifest meta.ManifestRecord, dryRun bool) error {
	_, err := r.cleanup.OCIReferences(ctx, manifest)
	if err == nil {
		return nil
	}
	if !errors.Is(err, object.ErrNotFound) {
		return fmt.Errorf("inspect OCI cleanup manifest %s: %w", manifest.Digest, err)
	}
	if dryRun || r.upstream == nil {
		return fmt.Errorf("OCI cleanup blocked for %s/%s@%s: restore the missing CAS body by verified digest before retrying (dry_run=%t, upstream_configured=%t): %w", manifest.Alias, manifest.Repository, manifest.Digest, dryRun, r.upstream != nil, err)
	}
	if recoveryErr := r.recoverCleanupManifest(ctx, manifest); recoveryErr != nil {
		return fmt.Errorf("OCI cleanup blocked: restore %s/%s@%s: %w", manifest.Alias, manifest.Repository, manifest.Digest, errors.Join(err, recoveryErr))
	}
	return nil
}

func (r *Runtime) recoverCleanupManifest(ctx context.Context, manifest meta.ManifestRecord) error {
	response, err := r.upstream.GetManifest(ctx, upstream.GetManifestRequest{
		UpstreamAlias: manifest.Alias, Repo: manifest.Repository,
		Reference: manifest.Digest, Accept: manifest.MediaType, Method: http.MethodGet,
	})
	if err != nil {
		return fmt.Errorf("fetch replacement OCI manifest: %w", err)
	}
	if response == nil || response.Body == nil {
		return errors.New("upstream returned no manifest body")
	}
	restoreErr := r.cleanup.RestoreOCIManifest(ctx, manifest, response.Body)
	return errors.Join(restoreErr, response.Body.Close())
}
