package config

import (
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
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
	var normalizeErr error
	sortedConfigAliases(c.Container).Range(func(_ int, alias string) bool {
		upstreamCfg, err := normalizeUpstreamConfig(alias, c.Container[alias].toUpstreamConfig())
		if err != nil {
			normalizeErr = err
			return false
		}
		c.Container[alias] = containerRegistryFromUpstreamConfig(upstreamCfg)
		out[alias] = upstreamCfg
		return true
	})
	return out, normalizeErr
}

func (c *Config) normalizeDependencyUpstreams(ecosystem string, upstreams DependencyEcosystemConfig) error {
	var normalizeErr error
	sortedConfigAliases(upstreams).Range(func(_ int, alias string) bool {
		upstreamCfg, err := normalizeUpstreamConfig(alias, upstreams[alias].toUpstreamConfig(ecosystem))
		if err != nil {
			normalizeErr = err
			return false
		}
		upstreams[alias] = dependencyUpstreamFromUpstreamConfig(upstreamCfg)
		return true
	})
	return normalizeErr
}

func sortedConfigAliases[T any](values map[string]T) *collectionlist.List[string] {
	return collectionlist.NewList(collectionmapping.NewMapFrom(values).Keys()...).
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
	return UpstreamConfig{
		Type:             ecosystemContainer,
		Registry:         cfg.Registry,
		Mirrors:          cfg.Mirrors,
		MirrorPolicy:     cfg.MirrorPolicy,
		DefaultNamespace: cfg.DefaultNamespace,
		TagTTL:           cfg.TagTTL,
		Blob:             cfg.Blob,
		Probe:            cfg.Probe,
		Auth:             cfg.Auth,
		HTTP:             cfg.HTTP,
	}
}

func containerRegistryFromUpstreamConfig(upstreamCfg UpstreamConfig) ContainerRegistryConfig {
	return ContainerRegistryConfig{
		Registry:         upstreamCfg.Registry,
		Mirrors:          upstreamCfg.Mirrors,
		MirrorPolicy:     upstreamCfg.MirrorPolicy,
		DefaultNamespace: upstreamCfg.DefaultNamespace,
		TagTTL:           upstreamCfg.TagTTL,
		Blob:             upstreamCfg.Blob,
		Probe:            upstreamCfg.Probe,
		Auth:             upstreamCfg.Auth,
		HTTP:             upstreamCfg.HTTP,
	}
}

func (cfg DependencyUpstreamConfig) toUpstreamConfig(ecosystem string) UpstreamConfig {
	return UpstreamConfig{
		Type:         ecosystem,
		Registry:     cfg.Registry,
		Mirrors:      cfg.Mirrors,
		MirrorPolicy: cfg.MirrorPolicy,
		TagTTL:       cfg.TagTTL,
		Probe:        cfg.Probe,
		Auth:         cfg.Auth,
		HTTP:         cfg.HTTP,
	}
}

func dependencyUpstreamFromUpstreamConfig(upstreamCfg UpstreamConfig) DependencyUpstreamConfig {
	return DependencyUpstreamConfig{
		Registry:     upstreamCfg.Registry,
		Mirrors:      upstreamCfg.Mirrors,
		MirrorPolicy: upstreamCfg.MirrorPolicy,
		TagTTL:       upstreamCfg.TagTTL,
		Probe:        upstreamCfg.Probe,
		Auth:         upstreamCfg.Auth,
		HTTP:         upstreamCfg.HTTP,
	}
}
