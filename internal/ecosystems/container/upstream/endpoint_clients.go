package upstream

import (
	"log/slog"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"go.uber.org/multierr"
)

// EndpointClients owns the HTTP clients expanded from upstream endpoint config.
type EndpointClients struct {
	groups *collectionmapping.OrderedMap[string, endpointClientGroup]
}

type endpointClientGroup struct {
	config   Config
	runtimes *collectionlist.List[upstreamRuntime]
}

func newEndpointClientsFromConfigs(
	configs *collectionmapping.OrderedMap[string, Config],
	logger *slog.Logger,
) *EndpointClients {
	groups := collectionmapping.NewOrderedMap[string, endpointClientGroup]()
	if configs == nil {
		return &EndpointClients{groups: groups}
	}

	groups = collectionmapping.NewOrderedMapWithCapacity[string, endpointClientGroup](configs.Len())
	configs.Range(func(alias string, cfg Config) bool {
		cfg.Alias = alias
		groups.Set(alias, endpointClientGroup{
			config:   cfg,
			runtimes: newEndpointRuntimes(cfg, logger),
		})
		return true
	})
	return &EndpointClients{groups: groups}
}

func newEndpointRuntimes(cfg Config, logger *slog.Logger) *collectionlist.List[upstreamRuntime] {
	registries := endpointRegistries(cfg)
	runtimes := collectionlist.NewListWithCapacity[upstreamRuntime](registries.Len())
	registries.Range(func(_ int, registry string) bool {
		runtimeCfg := cfg
		runtimeCfg.Registry = registry
		runtime := upstreamRuntime{config: runtimeCfg}
		runtime.client, runtime.err = newHTTPClient(runtimeCfg, logger)
		if runtime.err != nil && logger != nil {
			logger.Warn(
				"create upstream http client failed",
				"alias", cfg.Alias,
				"registry", registry,
				"error", runtime.err,
			)
		}
		runtimes.Add(runtime)
		return true
	})
	return runtimes
}

func (c *EndpointClients) Len() int {
	if c == nil || c.groups == nil {
		return 0
	}
	return c.groups.Len()
}

func (c *EndpointClients) Range(fn func(alias string, cfg Config, runtimes *collectionlist.List[upstreamRuntime]) bool) {
	if c == nil || c.groups == nil || fn == nil {
		return
	}
	c.groups.Range(func(alias string, group endpointClientGroup) bool {
		return fn(alias, group.config, group.runtimes)
	})
}

func (c *EndpointClients) Close() error {
	if c == nil || c.groups == nil {
		return nil
	}
	var closeErr error
	c.groups.Range(func(_ string, group endpointClientGroup) bool {
		if group.runtimes == nil {
			return true
		}
		group.runtimes.Range(func(_ int, runtime upstreamRuntime) bool {
			if runtime.client != nil {
				closeErr = multierr.Append(closeErr, runtime.client.Close())
			}
			return true
		})
		return true
	})
	if closeErr != nil {
		return wrapError(closeErr, "close upstream endpoint clients")
	}
	return nil
}
