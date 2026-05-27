// Package api exposes the RegiMux HTTP API endpoints.
package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/mo"
)

type HealthEndpoint struct{}

func NewHealthEndpoint() *HealthEndpoint {
	return &HealthEndpoint{}
}

func (e *HealthEndpoint) EndpointSpec() httpx.EndpointSpec {
	return endpointSpec("health")
}

func (e *HealthEndpoint) Register(registrar httpx.Registrar) {
	group := registrar.Scope()
	httpx.MustGroupGet(group, "healthz", e.health)
}

func (e *HealthEndpoint) health(context.Context, *struct{}) (*healthOutput, error) {
	out := &healthOutput{}
	out.Body.Status = "ok"
	return out, nil
}

type healthOutput struct {
	Body struct {
		Status string `json:"status"`
	} `json:"body"`
}

type RegistryEndpoint struct {
	manifests cache.ManifestService
	blobs     cache.BlobService
	tags      cache.TagService
	referrers cache.ReferrerService
	logger    *slog.Logger
	metrics   *observability.Metrics

	defaultNamespaces *collectionmapping.Map[string, string]
}

func NewRegistryEndpoint(
	manifests cache.ManifestService,
	blobs cache.BlobService,
	tags cache.TagService,
	referrers cache.ReferrerService,
	logger *slog.Logger,
) *RegistryEndpoint {
	if logger == nil {
		logger = slog.Default()
	}
	return &RegistryEndpoint{
		manifests:         manifests,
		blobs:             blobs,
		tags:              tags,
		referrers:         referrers,
		logger:            logger,
		defaultNamespaces: defaultNamespacesFromConfig(config.Config{}),
	}
}

func NewRegistryEndpointFromConfig(
	manifests cache.ManifestService,
	blobs cache.BlobService,
	tags cache.TagService,
	referrers cache.ReferrerService,
	logger *slog.Logger,
	cfg config.Config,
) *RegistryEndpoint {
	endpoint := NewRegistryEndpoint(manifests, blobs, tags, referrers, logger)
	endpoint.defaultNamespaces = defaultNamespacesFromConfig(cfg)
	return endpoint
}

type RegistryEndpointOptions struct {
	Config  config.Config
	Metrics *observability.Metrics
}

func NewRegistryEndpointFromOptions(
	manifests cache.ManifestService,
	blobs cache.BlobService,
	tags cache.TagService,
	referrers cache.ReferrerService,
	logger *slog.Logger,
	options RegistryEndpointOptions,
) *RegistryEndpoint {
	endpoint := NewRegistryEndpointFromConfig(manifests, blobs, tags, referrers, logger, options.Config)
	endpoint.metrics = options.Metrics
	return endpoint
}

func (e *RegistryEndpoint) SetMetrics(metrics *observability.Metrics) {
	if e == nil {
		return
	}
	e.metrics = metrics
}

func (e *RegistryEndpoint) EndpointSpec() httpx.EndpointSpec {
	return endpointSpec("registry")
}

func (e *RegistryEndpoint) Register(registrar httpx.Registrar) {
	group := registrar.Scope()
	httpx.MustGroupGet(group, "v2", e.ping)
	httpx.MustGroupGet(group, "v2/", e.ping, operationID("get-v2-slash"))
	httpx.MustGroupRoute(group, http.MethodHead, "v2", e.ping)
	httpx.MustGroupRoute(group, http.MethodHead, "v2/", e.ping, operationID("head-v2-slash"))
	httpx.MustGroupGet(group, "v2/{alias}/{tail...}", e.get, registryOperationDocs()...)
	httpx.MustGroupRoute(group, http.MethodHead, "v2/{alias}/{tail...}", e.head, registryOperationDocs()...)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		httpx.MustGroupRoute(group, method, "v2/{alias}/{tail...}", func(_ context.Context, input *registryInput) (*registryOutput, error) {
			return errorOutput(unsupported(method, input.path())), nil
		}, registryOperationDocs()...)
	}
}

func (e *RegistryEndpoint) ping(context.Context, *struct{}) (*registryOutput, error) {
	return &registryOutput{
		Status:                       http.StatusOK,
		ContentLength:                "0",
		DockerDistributionAPIVersion: distribution.APIVersion,
		Body:                         streamWithStatus(http.StatusOK, httpx.StreamBytes(nil)),
	}, nil
}

func (e *RegistryEndpoint) get(ctx context.Context, input *registryInput) (*registryOutput, error) {
	return e.dispatch(ctx, input, http.MethodGet)
}

func (e *RegistryEndpoint) head(ctx context.Context, input *registryInput) (*registryOutput, error) {
	return e.dispatch(ctx, input, http.MethodHead)
}

func (e *RegistryEndpoint) dispatch(ctx context.Context, input *registryInput, method string) (*registryOutput, error) {
	startedAt := time.Now()
	routeName := "registry.invalid"
	route, err := routeFromInput(input)
	if err != nil {
		if errors.Is(err, reference.ErrDigestInvalid) {
			out := errorOutput(distribution.ErrDigestInvalid.WithDetail(err.Error()))
			e.observeAPI(routeName, method, out, time.Since(startedAt), nil)
			return out, nil
		}
		out := errorOutput(distribution.ErrNameInvalid.WithDetail(err.Error()))
		e.observeAPI(routeName, method, out, time.Since(startedAt), nil)
		return out, nil
	}
	route = route.WithDefaultNamespace(e.defaultNamespace(route.Alias).OrEmpty())
	routeName = registryRouteName(route.Kind)

	var out *registryOutput
	switch route.Kind {
	case reference.RoutePing:
		out, err = e.ping(ctx, nil)
	case reference.RouteManifest:
		out = e.manifest(ctx, input, route, method)
	case reference.RouteBlob:
		out = e.blob(ctx, input, route, method)
	case reference.RouteTags:
		out = e.tagsRoute(ctx, input, route, method)
	case reference.RouteReferrers:
		out = e.referrersRoute(ctx, input, route, method)
	default:
		out = errorOutput(distribution.ErrNameInvalid.WithDetail("unknown registry route"))
	}
	e.observeAPI(routeName, method, out, time.Since(startedAt), err)
	return out, err
}

func (e *RegistryEndpoint) defaultNamespace(alias string) mo.Option[string] {
	if e == nil || e.defaultNamespaces == nil {
		return mo.None[string]()
	}
	return e.defaultNamespaces.GetOption(alias)
}

func (e *RegistryEndpoint) manifest(ctx context.Context, input *registryInput, route reference.Route, method string) *registryOutput {
	result, err := e.manifests.Get(ctx, cache.ManifestRequest{
		UpstreamAlias: route.Alias,
		Repo:          route.Repo,
		Reference:     route.Reference,
		Accept:        input.Accept,
		Method:        method,
	})
	if err != nil {
		return errorOutput(distribution.FromError(err))
	}

	out := newRegistryOutput(http.StatusOK, result.Headers)
	out.ContentType = result.MediaType
	out.DockerContentDigest = result.Digest
	out.XMirrorCache = string(result.Cache)
	if result.Size >= 0 {
		out.ContentLength = strconv.FormatInt(result.Size, 10)
	}
	if method != http.MethodHead {
		out.Body = streamWithStatus(out.Status, httpx.StreamBytes(result.Body))
	}
	return out
}

func (e *RegistryEndpoint) blob(ctx context.Context, input *registryInput, route reference.Route, method string) *registryOutput {
	httpRange, err := reference.ParseRange(input.Range)
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

	status := result.Status
	if status == 0 {
		status = http.StatusOK
	}
	out := newRegistryOutput(status, result.Headers)
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

type registryInput struct {
	Alias  string         `path:"alias"`
	Tail   httpx.PathTail `path:"tail"`
	Accept string         `header:"Accept"`
	Range  string         `header:"Range"`
	N      string         `query:"n"`
	Last   string         `query:"last"`
}

var (
	_ httpx.Endpoint             = (*HealthEndpoint)(nil)
	_ httpx.Endpoint             = (*RegistryEndpoint)(nil)
	_ httpx.EndpointSpecProvider = (*HealthEndpoint)(nil)
	_ httpx.EndpointSpecProvider = (*RegistryEndpoint)(nil)
)

func (e *RegistryEndpoint) observeAPI(route, method string, out *registryOutput, duration time.Duration, err error) {
	if e == nil || e.metrics == nil {
		return
	}
	status := http.StatusInternalServerError
	if out != nil && out.Status != 0 {
		status = out.Status
	}
	e.metrics.ObserveAPIRequest(route, method, status, duration, err)
}

func registryRouteName(kind reference.RouteKind) string {
	if kind == "" {
		return "registry.unknown"
	}
	return "registry." + string(kind)
}
