package container

import (
	"context"
	"net/http"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/oops"
)

func (r *Runtime) Refresh(ctx context.Context, req ecosystem.RefreshRequest) error {
	if r == nil || r.manifestRefresh == nil {
		return oops.In("container").With("ecosystem", ecosystem.Container).Errorf("container refresh service is not configured")
	}
	if kind := strings.TrimSpace(req.Kind); kind != "" && kind != "manifest" {
		return nil
	}
	accept := strings.TrimSpace(req.Accept)
	if accept == "" {
		accept = distribution.DefaultManifestAccept
	}
	manifest, err := r.manifestRefresh.Refresh(ctx, cache.ManifestRequest{
		UpstreamAlias: req.Alias,
		Repo:          req.Repository,
		Reference:     req.Reference,
		Accept:        accept,
		Method:        http.MethodGet,
	})
	if err != nil {
		return oops.Wrapf(err, "refresh container manifest")
	}
	if manifest == nil {
		return oops.In("container").Errorf("container refresh response is empty")
	}
	return nil
}
