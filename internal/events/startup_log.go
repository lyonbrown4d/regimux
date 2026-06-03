package events

import (
	"context"
	"log/slog"
	"net"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionset "github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
)

const (
	defaultStartupBaseURL = "http://localhost:5000"
	defaultPprofPath      = "/debug/pprof"
)

type startupEndpoint struct {
	name string
	url  string
}

func logStartup(_ context.Context, cfg config.Config, logger *slog.Logger, version build.Version) error {
	ordered := cfg.OrderedContainerUpstreams()
	logger = startupLogger(logger)
	logger.Info("regimuxd starting",
		"version", string(version),
		"listen", cfg.Server.Listen,
		"public_url", serviceBaseURL(cfg.Server),
		"container_upstream_count", ordered.Len(),
		"container_upstreams", ordered.Keys(),
	)
	return nil
}

func logRuntimeAccess(_ context.Context, cfg config.Config, logger *slog.Logger) error {
	logger = startupLogger(logger)
	startupServiceEndpoints(cfg).Range(func(_ int, endpoint startupEndpoint) bool {
		logger.Info("service endpoint available", "name", endpoint.name, "url", endpoint.url)
		return true
	})
	logRegisteredUpstreams(logger, cfg)
	return nil
}

func logRegisteredUpstreams(logger *slog.Logger, cfg config.Config) {
	cfg.OrderedContainerUpstreams().Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		endpoints := upstreamEndpointRegistries(upstreamCfg)
		logger.Info("registry upstream registered",
			"alias", alias,
			"registry", cleanRegistry(upstreamCfg.Registry),
			"mirrors", upstreamCfg.Mirrors,
			"endpoint_count", len(endpoints),
			"endpoints", endpoints,
			"mirror_policy", upstreamCfg.MirrorPolicy,
			"blob_mirror_policy", upstreamCfg.Blob.MirrorPolicy,
			"default_namespace", upstreamCfg.DefaultNamespace,
			"auth_type", upstreamCfg.Auth.Type,
			"probe_enabled", upstreamCfg.Probe.Enabled,
		)
		return true
	})
}

func startupServiceEndpoints(cfg config.Config) *collectionlist.List[startupEndpoint] {
	base := serviceBaseURL(cfg.Server)
	endpoints := collectionlist.NewList(
		startupEndpoint{name: "registry", url: joinStartupURL(base, "/v2/")},
		startupEndpoint{name: "health", url: joinStartupURL(base, "/healthz")},
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
	return endpoints
}

func upstreamEndpointRegistries(cfg config.UpstreamConfig) []string {
	registries := collectionset.NewOrderedSetWithCapacity[string](len(cfg.Mirrors) + 1)
	collectionlist.NewList(cfg.Mirrors...).Range(func(_ int, registry string) bool {
		if endpoint := cleanRegistry(registry); endpoint != "" {
			registries.Add(endpoint)
		}
		return true
	})
	if registry := cleanRegistry(cfg.Registry); registry != "" {
		registries.Add(registry)
	}
	return registries.Values()
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
