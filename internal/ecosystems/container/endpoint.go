package container

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/suggestion"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/observability"
	accesspolicy "github.com/lyonbrown4d/regimux/internal/policy"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/mo"
)

type RegistryEndpoint struct {
	manifests   cache.ManifestService
	blobs       cache.BlobService
	tags        cache.TagService
	referrers   cache.ReferrerService
	logger      *slog.Logger
	metrics     *observability.Metrics
	suggestions suggestion.ManifestService
	metadata    meta.Store
	events      events.Bus
	workers     *worker.Pools

	defaultNamespaces     *collectionmapping.Map[string, string]
	defaultContainerAlias string
	containerAliases      map[string]struct{}
	dependencyPolicy      accesspolicy.DependencyPolicy
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
	endpoint.defaultContainerAlias = cfg.ContainerDefaultAlias()
	endpoint.containerAliases = make(map[string]struct{}, len(cfg.Container))
	for alias := range cfg.Container {
		endpoint.containerAliases[alias] = struct{}{}
	}
	return endpoint
}

type RegistryEndpointOptions struct {
	Config      config.Config
	Metrics     *observability.Metrics
	Suggestions suggestion.ManifestService
	Metadata    meta.Store
	Events      events.Bus
	Workers     *worker.Pools
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
	endpoint.suggestions = options.Suggestions
	endpoint.metadata = options.Metadata
	endpoint.events = options.Events
	endpoint.workers = options.Workers
	endpoint.dependencyPolicy = accesspolicy.FromConfig(options.Config.Policy.Dependency)
	return endpoint
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
	route, err := e.routeFromInput(input)
	if err != nil {
		if errors.Is(err, reference.ErrDigestInvalid) {
			out := errorOutput(distribution.ErrDigestInvalid.WithDetail(err.Error()))
			e.observeAPI(ctx, routeName, reference.Route{}, method, out, time.Since(startedAt), nil)
			return out, nil
		}
		out := errorOutput(distribution.ErrNameInvalid.WithDetail(err.Error()))
		e.observeAPI(ctx, routeName, reference.Route{}, method, out, time.Since(startedAt), nil)
		return out, nil
	}
	route = route.WithDefaultNamespace(e.defaultNamespace(route.Alias).OrEmpty())
	routeName = registryRouteName(route.Kind)
	if policyErr := e.checkDependencyPolicy(route); policyErr != nil {
		e.recordPolicyDeniedPull(ctx, route, policyErr)
		out := errorOutput(distribution.ErrDenied.WithDetail(policyErr.Error()))
		e.observeAPI(ctx, routeName, route, method, out, time.Since(startedAt), policyErr)
		return out, nil
	}

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
	e.observeAPI(ctx, routeName, route, method, out, time.Since(startedAt), err)
	return out, err
}

func (e *RegistryEndpoint) checkDependencyPolicy(route reference.Route) error {
	if route.Kind == reference.RoutePing {
		return nil
	}
	if err := e.dependencyPolicy.Check(accesspolicy.DependencyTarget{
		Ecosystem: ecosystem.Container,
		Alias:     route.Alias,
		Artifact:  route.Repo,
		Reference: containerPolicyReference(route),
	}); err != nil {
		return wrapError(err, "check container dependency policy")
	}
	return nil
}

func containerPolicyReference(route reference.Route) string {
	switch route.Kind {
	case reference.RoutePing:
		return ""
	case reference.RouteManifest:
		return route.Reference
	case reference.RouteBlob, reference.RouteReferrers:
		return route.Digest
	case reference.RouteTags:
		return "tags"
	default:
		return route.Reference
	}
}

func (e *RegistryEndpoint) recordPolicyDeniedPull(ctx context.Context, route reference.Route, err error) {
	if e == nil ||
		route.Kind == reference.RoutePing ||
		!errors.Is(err, accesspolicy.ErrDependencyBlocked) {
		return
	}
	key := meta.PullKey{
		Alias:      route.Alias,
		Repository: route.Repo,
		Reference:  containerPolicyReference(route),
	}
	if key.Reference == "" {
		key.Reference = route.Reference
	}
	if e.metadata != nil {
		if _, recordErr := e.metadata.RecordPolicyDeniedPull(ctx, key, time.Now().UTC()); recordErr != nil && e.logger != nil {
			e.logger.DebugContext(ctx, "record container proxy policy denied pull failed", "alias", key.Alias, "repository", key.Repository, "reference", key.Reference, "error", recordErr)
		}
	}
	e.publishDependencyPullDenied(ctx, route, key, err)
}

func (e *RegistryEndpoint) publishDependencyPullDenied(ctx context.Context, route reference.Route, key meta.PullKey, denyErr error) {
	if e == nil || e.events == nil {
		return
	}
	reason := ""
	if denyErr != nil {
		reason = denyErr.Error()
	}
	if err := events.Publish(ctx, e.events, events.DependencyPullDenied{
		Ecosystem:  ecosystem.Container,
		Kind:       string(route.Kind),
		Alias:      key.Alias,
		Repository: key.Repository,
		Reference:  key.Reference,
		Reason:     reason,
	}); err != nil && e.logger != nil {
		e.logger.DebugContext(ctx, "publish container proxy policy denied pull event failed", "alias", key.Alias, "repository", key.Repository, "reference", key.Reference, "error", err)
	}
}

func (e *RegistryEndpoint) defaultNamespace(alias string) mo.Option[string] {
	if e == nil || e.defaultNamespaces == nil {
		return mo.None[string]()
	}
	return e.defaultNamespaces.GetOption(alias)
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
	_ httpx.Endpoint             = (*RegistryEndpoint)(nil)
	_ httpx.EndpointSpecProvider = (*RegistryEndpoint)(nil)
)
