package container

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/lyonbrown4d/regimux/internal/artifactstream"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/lo"
)

func (e *RegistryEndpoint) manifest(
	ctx context.Context,
	input *registryInput,
	route reference.Route,
	method string,
) (*registryOutput, func() error) {
	result, complete, err := e.prepareManifest(ctx, cache.ManifestRequest{
		UpstreamAlias: route.Alias,
		Repo:          route.Repo,
		Reference:     route.Reference,
		Accept:        input.Accept,
		Method:        method,
	})
	if err != nil {
		return e.manifestError(ctx, route, err), nil
	}
	e.observeManifest(ctx, route)

	out := newRegistryOutput(http.StatusOK, result.Headers)
	out.ContentType = result.MediaType
	out.DockerContentDigest = result.Digest
	out.XMirrorCache = string(result.Cache)
	if result.Size >= 0 {
		out.ContentLength = strconv.FormatInt(result.Size, 10)
	}
	if method != http.MethodHead {
		stream, cleanup, streamErr := e.streamMetadataWithCleanup(ctx, result.Body)
		if streamErr != nil {
			if complete == nil {
				return errorOutput(distribution.FromError(streamErr)), nil
			}
			finalize := func() error { return errors.Join(streamErr, complete.Finalize(ctx)) }
			return errorOutput(distribution.FromError(streamErr)), finalize
		}
		out.Body = streamWithStatus(out.Status, stream)
		out.cleanup = cleanup
		return out, manifestCompletion(ctx, complete, e.prepareManifestBlobFill(ctx, route, result))
	}
	return out, manifestCompletion(ctx, complete, nil)
}

func (e *RegistryEndpoint) blob(ctx context.Context, input *registryInput, route reference.Route, method string) *registryOutput {
	httpRange, err := object.ParseRange(input.Range)
	if err != nil {
		return errorOutput(distribution.ErrRangeInvalid.WithDetail(err.Error()))
	}
	result, err := e.blobs.Get(ctx, cache.BlobRequest{
		UpstreamAlias: route.Alias,
		Repo:          route.Repo,
		Digest:        route.Digest,
		Range:         httpRange,
		Method:        method,
	})
	if err != nil {
		return errorOutput(distribution.FromError(err))
	}

	out := newRegistryOutput(lo.CoalesceOrEmpty(result.Status, http.StatusOK), result.Headers)
	out.ContentType = distribution.MediaTypeOctetStream
	out.DockerContentDigest = result.Digest
	out.AcceptRanges = distribution.RangeUnitBytes
	out.XMirrorCache = string(result.Cache)
	if method == http.MethodHead {
		if err := result.Reader.Close(); err != nil {
			return errorOutput(distribution.ErrUnknown.WithDetail(err.Error()))
		}
		return out
	}
	out.Body = artifactstream.Response(ctx, artifactstream.ResponseRequest{
		Status: out.Status, Size: result.Size, Body: result.Reader, Logger: e.logger,
		Ecosystem: "oci", Alias: route.Alias, Reference: route.Digest, Cache: string(result.Cache),
	})
	return out
}

func (e *RegistryEndpoint) tagList(ctx context.Context, input *registryInput, route reference.Route) *registryOutput {
	result, err := e.tags.List(ctx, cache.TagRequest{
		UpstreamAlias: route.Alias,
		Repo:          route.Repo,
		N:             input.N,
		Last:          input.Last,
	})
	if err != nil {
		return errorOutput(distribution.FromError(err))
	}

	out := newRegistryOutput(http.StatusOK, result.Headers)
	out.ContentType = distribution.MediaTypeJSON
	out.XMirrorCache = string(result.Cache)
	stream, err := e.streamMetadata(ctx, result.Body)
	if err != nil {
		return errorOutput(distribution.FromError(err))
	}
	out.Body = streamWithStatus(out.Status, stream)
	return out
}

func (e *RegistryEndpoint) tagsRoute(ctx context.Context, input *registryInput, route reference.Route, method string) *registryOutput {
	if method != http.MethodGet {
		return errorOutput(unsupported(method, input.path()))
	}
	return e.tagList(ctx, input, route)
}

func (e *RegistryEndpoint) referrersList(ctx context.Context, route reference.Route) *registryOutput {
	result, err := e.referrers.Get(ctx, cache.ReferrerRequest{
		UpstreamAlias: route.Alias,
		Repo:          route.Repo,
		Digest:        route.Digest,
	})
	if err != nil {
		return errorOutput(distribution.FromError(err))
	}

	out := newRegistryOutput(http.StatusOK, result.Headers)
	out.ContentType = result.MediaType
	out.XMirrorCache = string(result.Cache)
	stream, err := e.streamMetadata(ctx, result.Body)
	if err != nil {
		return errorOutput(distribution.FromError(err))
	}
	out.Body = streamWithStatus(out.Status, stream)
	return out
}

func (e *RegistryEndpoint) referrersRoute(ctx context.Context, input *registryInput, route reference.Route, method string) *registryOutput {
	if method != http.MethodGet {
		return errorOutput(unsupported(method, input.path()))
	}
	return e.referrersList(ctx, route)
}
