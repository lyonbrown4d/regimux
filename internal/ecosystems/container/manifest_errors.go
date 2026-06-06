package container

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/suggestion"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func (e *RegistryEndpoint) manifestError(ctx context.Context, route reference.Route, err error) *registryOutput {
	if !isManifestUnknown(err) || e == nil || e.suggestions == nil {
		return errorOutput(distribution.FromError(err))
	}
	suggestions := e.suggestions.SuggestManifest(ctx, suggestion.ManifestRequest{
		Alias:      route.Alias,
		Repository: route.Repo,
		Reference:  route.Reference,
	})
	if suggestions.Empty() {
		return errorOutput(distribution.FromError(err))
	}
	return errorOutput(distribution.ManifestUnknownWithSuggestions(
		route.Alias,
		route.Repo,
		route.Reference,
		suggestions.Tags,
		suggestions.Repositories,
	))
}

func (e *RegistryEndpoint) observeManifest(ctx context.Context, route reference.Route) {
	if e == nil || e.suggestions == nil {
		return
	}
	e.suggestions.ObserveManifest(ctx, suggestion.ManifestRequest{
		Alias:      route.Alias,
		Repository: route.Repo,
		Reference:  route.Reference,
	})
}

func isManifestUnknown(err error) bool {
	list := distribution.FromError(err)
	if list == nil {
		return false
	}
	for _, item := range list.Errors {
		if item.Code == distribution.CodeManifestUnknown {
			return true
		}
	}
	return false
}
