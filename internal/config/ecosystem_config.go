package config

import (
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/samber/lo"
	"github.com/samber/oops"
)

const (
	ecosystemContainer = "oci"
	ecosystemGo        = "go"
	ecosystemMaven     = "maven"
	ecosystemNPM       = "npm"
	ecosystemPyPI      = "pypi"
)

func (c *Config) normalizeUpstreams() error {
	containerUpstreams, err := c.normalizeContainerUpstreams()
	if err != nil {
		return err
	}
	c.Upstreams = containerUpstreams

	if err := c.normalizeDependencyUpstreams(ecosystemGo, c.ensureGoConfig()); err != nil {
		return err
	}
	if err := c.normalizeDependencyUpstreams(ecosystemMaven, c.ensureMavenConfig()); err != nil {
		return err
	}
	if err := c.normalizeDependencyUpstreams(ecosystemNPM, c.ensureNPMConfig()); err != nil {
		return err
	}
	if err := c.normalizeDependencyUpstreams(ecosystemPyPI, c.ensurePyPIConfig()); err != nil {
		return err
	}
	if len(c.Upstreams)+len(c.Go)+len(c.Maven)+len(c.NPM)+len(c.PyPI) == 0 {
		return oops.In("config").Errorf("at least one ecosystem upstream is required")
	}
	return nil
}

func (c *Config) normalizeContainerUpstreams() (map[string]UpstreamConfig, error) {
	out := map[string]UpstreamConfig{}
	mapper := newUpstreamConfigMapper()
	var normalizeErr error
	sortedConfigAliases(c.Container).Range(func(_ int, alias string) bool {
		upstreamCfg, err := mapper.ContainerRegistryToUpstream(c.Container[alias])
		if err != nil {
			normalizeErr = err
			return false
		}
		upstreamCfg, err = normalizeUpstreamConfig(alias, upstreamCfg)
		if err != nil {
			normalizeErr = err
			return false
		}
		containerCfg, err := mapper.UpstreamToContainerRegistry(upstreamCfg)
		if err != nil {
			normalizeErr = err
			return false
		}
		c.Container[alias] = containerCfg
		out[alias] = upstreamCfg
		return true
	})
	return out, normalizeErr
}

func (c *Config) normalizeDependencyUpstreams(ecosystem string, upstreams DependencyEcosystemConfig) error {
	mapper := newUpstreamConfigMapper()
	var normalizeErr error
	sortedConfigAliases(upstreams).Range(func(_ int, alias string) bool {
		upstreamCfg, err := mapper.DependencyUpstreamToUpstream(upstreams[alias], ecosystem)
		if err != nil {
			normalizeErr = err
			return false
		}
		upstreamCfg, err = normalizeUpstreamConfig(alias, upstreamCfg)
		if err != nil {
			normalizeErr = err
			return false
		}
		dependencyCfg, err := mapper.UpstreamToDependencyUpstream(upstreamCfg)
		if err != nil {
			normalizeErr = err
			return false
		}
		upstreams[alias] = dependencyCfg
		return true
	})
	return normalizeErr
}

func sortedConfigAliases[T any](values map[string]T) *collectionlist.List[string] {
	return collectionlist.NewList(lo.Keys(values)...).
		Sort(strings.Compare)
}

func (c *Config) ensureGoConfig() DependencyEcosystemConfig {
	if c.Go == nil {
		c.Go = DependencyEcosystemConfig{}
	}
	return c.Go
}

func (c *Config) ensureMavenConfig() DependencyEcosystemConfig {
	if c.Maven == nil {
		c.Maven = DependencyEcosystemConfig{}
	}
	return c.Maven
}

func (c *Config) ensureNPMConfig() DependencyEcosystemConfig {
	if c.NPM == nil {
		c.NPM = DependencyEcosystemConfig{}
	}
	return c.NPM
}

func (c *Config) ensurePyPIConfig() DependencyEcosystemConfig {
	if c.PyPI == nil {
		c.PyPI = DependencyEcosystemConfig{}
	}
	return c.PyPI
}

func (cfg ContainerRegistryConfig) toUpstreamConfig() UpstreamConfig {
	return mustMapUpstreamConfig(newUpstreamConfigMapper().ContainerRegistryToUpstream(cfg))
}

func containerRegistryFromUpstreamConfig(upstreamCfg UpstreamConfig) ContainerRegistryConfig {
	return mustMapUpstreamConfig(newUpstreamConfigMapper().UpstreamToContainerRegistry(upstreamCfg))
}

func (cfg DependencyUpstreamConfig) toUpstreamConfig(ecosystem string) UpstreamConfig {
	return mustMapUpstreamConfig(newUpstreamConfigMapper().DependencyUpstreamToUpstream(cfg, ecosystem))
}

func dependencyUpstreamFromUpstreamConfig(upstreamCfg UpstreamConfig) DependencyUpstreamConfig {
	return mustMapUpstreamConfig(newUpstreamConfigMapper().UpstreamToDependencyUpstream(upstreamCfg))
}
