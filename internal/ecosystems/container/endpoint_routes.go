package container

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/lo"
)

func (e *RegistryEndpoint) manifest(ctx context.Context, input *registryInput, route reference.Route, method string) *registryOutput {
	result, err := e.manifests.Get(ctx, cache.ManifestRequest{
		UpstreamAlias: route.Alias,
		Repo:          route.Repo,
		Reference:     route.Reference,
		Accept:        input.Accept,
		Method:        method,
	})
	if err != nil {
		return e.manifestError(ctx, route, err)
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
		out.Body = streamWithStatus(out.Status, httpx.StreamBytes(result.Body))
		e.fillManifestBlobsAsync(ctx, route, result)
	}
	return out
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
	out.Body = streamWithStatus(out.Status, httpx.StreamWriter(func(writer io.Writer) {
		e.writeBlobBody(writer, result.Reader)
	}))
	return out
}

func (e *RegistryEndpoint) writeBlobBody(writer io.Writer, reader io.ReadCloser) {
	if _, err := io.Copy(writer, reader); err != nil {
		e.logger.Error("write blob response failed", "error", err)
	}
	if err := reader.Close(); err != nil {
		e.logger.Error("close blob response reader failed", "error", err)
	}
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
	out.Body = streamWithStatus(out.Status, httpx.StreamBytes(result.Body))
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
	out.Body = streamWithStatus(out.Status, httpx.StreamBytes(result.Body))
	return out
}

func (e *RegistryEndpoint) referrersRoute(ctx context.Context, input *registryInput, route reference.Route, method string) *registryOutput {
	if method != http.MethodGet {
		return errorOutput(unsupported(method, input.path()))
	}
	return e.referrersList(ctx, route)
}
