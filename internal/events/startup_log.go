package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/lo"
)

const (
	defaultStartupBaseURL = "http://localhost:8080"
	defaultPprofPath      = "/debug/pprof"
)

type startupEndpoint struct {
	name    string
	url     string
	aliases []string
}

func logStartup(_ context.Context, deps StartupDependencies) error {
	upstreams := ecosystem.ConfiguredUpstreams(deps.Runtimes)
	logger := startupLogger(deps.Logger)
	logger.Info("parsed config", "config", deps.Config)
	if configJSON, err := json.MarshalIndent(deps.Config, "", "  "); err == nil {
		logger.Info(fmt.Sprintf("parsed config (pretty)\n%s", configJSON))
	} else {
		logger.Error("failed to render parsed config as pretty json", "error", err)
	}
	logger.Info("regimuxd starting",
		"version", string(deps.Version),
		"listen", deps.Config.Server.Listen,
		"public_url", serviceBaseURL(deps.Config.Server),
		"upstream_count", upstreams.Len(),
		"upstreams", startupUpstreamLabels(upstreams).Values(),
	)
	return nil
}

func logRuntimeAccess(_ context.Context, deps RuntimeAccessDependencies) error {
	logger := startupLogger(deps.Logger)
	startupServiceEndpoints(deps.Config, deps.Runtimes).Range(func(_ int, endpoint startupEndpoint) bool {
		fields := []any{
			"name", endpoint.name,
			"url", endpoint.url,
		}
		if len(endpoint.aliases) > 0 {
			fields = append(fields, "aliases", endpoint.aliases)
		}
		logger.Info("service endpoint available", fields...)
		return true
	})
	logRegisteredUpstreams(logger, deps.Runtimes)
	return nil
}

func logRegisteredUpstreams(logger *slog.Logger, runtimes *collectionlist.List[ecosystem.Runtime]) {
	ecosystem.ConfiguredUpstreams(runtimes).Range(func(_ int, upstream ecosystem.Upstream) bool {
		upstreamCfg := upstream.Config
		endpoints := upstreamEndpointRegistries(upstreamCfg)
		logger.Info("upstream registered",
			"ecosystem", upstream.Ecosystem,
			"alias", upstream.Alias,
			"registry", cleanRegistry(upstreamCfg.Registry),
			"mirrors", upstreamCfg.Mirrors,
			"endpoint_count", endpoints.Len(),
			"endpoints", endpoints.Values(),
			"mirror_policy", upstreamCfg.MirrorPolicy,
			"blob_mirror_policy", upstreamCfg.Blob.MirrorPolicy,
			"default_namespace", upstreamCfg.DefaultNamespace,
			"auth_type", upstreamCfg.Auth.Type,
			"probe_enabled", upstreamCfg.Probe.Enabled,
		)
		return true
	})
}

func startupServiceEndpoints(cfg config.Config, runtimes *collectionlist.List[ecosystem.Runtime]) *collectionlist.List[startupEndpoint] {
	base := serviceBaseURL(cfg.Server)
	endpoints := collectionlist.NewList(
		startupEndpoint{name: "registry", url: joinStartupURL(base, "/v2/")},
		startupEndpoint{name: "prometheus", url: joinStartupURL(base, "/metrics")},
		startupEndpoint{name: "admin", url: joinStartupURL(base, "/admin")},
		startupEndpoint{name: "docs", url: joinStartupURL(base, "/docs")},
		startupEndpoint{name: "openapi", url: joinStartupURL(base, "/openapi.json")},
	)
	if cfg.Auth.Enabled {
		endpoints.Add(startupEndpoint{name: "auth_token", url: joinStartupURL(base, "/auth/token")})
	}
	if cfg.Server.Middleware.Healthcheck.Enabled {
		endpoints.Add(startupEndpoint{name: "liveness", url: joinStartupURL(base, cfg.Server.Middleware.Healthcheck.LivenessPath)})
		endpoints.Add(startupEndpoint{name: "readiness", url: joinStartupURL(base, cfg.Server.Middleware.Healthcheck.ReadinessPath)})
	}
	if cfg.Server.Middleware.Pprof.Enabled {
		endpoints.Add(startupEndpoint{name: "pprof", url: joinStartupURL(base, startupPprofPath(cfg.Server.Middleware.Pprof))})
	}
	endpoints.Add(dependencyStartupEndpoints(base, ecosystem.ConfiguredUpstreams(runtimes)).Values()...)
	return endpoints
}

func dependencyStartupEndpoints(base string, upstreams *collectionlist.List[ecosystem.Upstream]) *collectionlist.List[startupEndpoint] {
	groups := collectionmapping.NewMap[string, *collectionlist.List[string]]()
	if upstreams != nil {
		upstreams.Range(func(_ int, upstream ecosystem.Upstream) bool {
			if upstream.Ecosystem == "" || upstream.Ecosystem == ecosystem.Container {
				return true
			}
			aliases, ok := groups.Get(upstream.Ecosystem)
			if !ok {
				aliases = collectionlist.NewList[string]()
				groups.Set(upstream.Ecosystem, aliases)
			}
			aliases.Add(upstream.Alias)
			return true
		})
	}

	names := collectionlist.NewList(groups.Keys()...).Sort(strings.Compare)
	return collectionlist.MapList(names, func(_ int, name string) startupEndpoint {
		aliases, _ := groups.Get(name)
		return startupEndpoint{
			name:    name,
			url:     joinStartupURL(base, "/"+name),
			aliases: sortedStartupAliases(aliases),
		}
	})
}

func sortedStartupAliases(aliases *collectionlist.List[string]) []string {
	if aliases == nil {
		return nil
	}
	return aliases.Sort(strings.Compare).Values()
}

func startupUpstreamLabels(upstreams *collectionlist.List[ecosystem.Upstream]) *collectionlist.List[string] {
	if upstreams == nil {
		return collectionlist.NewList[string]()
	}
	return collectionlist.MapList(upstreams, func(_ int, upstream ecosystem.Upstream) string {
		return upstreamDisplayName(upstream)
	})
}

func upstreamDisplayName(upstream ecosystem.Upstream) string {
	if upstream.Ecosystem == "" || upstream.Ecosystem == ecosystem.Container {
		return upstream.Alias
	}
	return upstream.Ecosystem + "/" + upstream.Alias
}

func upstreamEndpointRegistries(cfg config.UpstreamConfig) *collectionlist.List[string] {
	registries := lo.FilterMap(lo.Concat(cfg.Mirrors, []string{cfg.Registry}), func(registry string, _ int) (string, bool) {
		endpoint := cleanRegistry(registry)
		return endpoint, endpoint != ""
	})
	return collectionlist.NewList(lo.Uniq(registries)...)
}

func serviceBaseURL(cfg config.ServerConfig) string {
	if publicURL := strings.TrimRight(strings.TrimSpace(cfg.PublicURL), "/"); publicURL != "" {
		return publicURL
	}
	return listenBaseURL(cfg.Listen)
}

func listenBaseURL(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return defaultStartupBaseURL
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		host, port = fallbackHostPort(listen)
	}
	host = startupHost(host)
	if port == "" {
		return "http://" + host
	}
	return "http://" + net.JoinHostPort(host, port)
}

func fallbackHostPort(listen string) (string, string) {
	if port, ok := strings.CutPrefix(listen, ":"); ok {
		return "", port
	}
	host, port, ok := strings.Cut(listen, ":")
	if ok {
		return host, port
	}
	return listen, ""
}

func startupHost(host string) string {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	switch host {
	case "", "0.0.0.0", "::":
		return "localhost"
	default:
		return host
	}
}

func joinStartupURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	path = "/" + strings.TrimLeft(strings.TrimSpace(path), "/")
	if base == "" {
		base = defaultStartupBaseURL
	}
	return base + path
}

func startupPprofPath(cfg config.MiddlewarePprofConfig) string {
	prefix := strings.TrimSpace(cfg.Prefix)
	if prefix == "" {
		return defaultPprofPath
	}
	return prefix
}

func cleanRegistry(registry string) string {
	return strings.TrimRight(strings.TrimSpace(registry), "/")
}

func startupLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return logger.With("component", "startup")
}
