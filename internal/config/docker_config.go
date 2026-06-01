package config

import (
	"net/url"
	"strings"
	"time"

	"github.com/samber/oops"
)

const defaultDockerPrewarmTimeout = 10 * time.Minute

func (c *Config) normalizeDocker() error {
	c.Docker.Host = strings.TrimSpace(c.Docker.Host)
	c.Docker.Prewarm.Alias = strings.TrimSpace(c.Docker.Prewarm.Alias)
	if c.Docker.Prewarm.Alias == "" {
		c.Docker.Prewarm.Alias = "hub"
	}
	if c.Docker.Prewarm.Timeout == 0 {
		c.Docker.Prewarm.Timeout = defaultDockerPrewarmTimeout
	}
	c.Docker.Prewarm.Platform = strings.TrimSpace(c.Docker.Prewarm.Platform)
	for i, image := range c.Docker.Prewarm.Images {
		c.Docker.Prewarm.Images[i] = strings.TrimSpace(image)
	}
	c.Docker.Prewarm.Images = uniqueStrings(c.Docker.Prewarm.Images)

	registry, err := normalizeDockerRegistry(c.Docker.Prewarm.Registry, c.Server.PublicURL)
	if err != nil {
		return err
	}
	c.Docker.Prewarm.Registry = registry
	if err := c.validateDockerPrewarmAlias(); err != nil {
		return err
	}
	return nil
}

func (c Config) validateDockerPrewarmAlias() error {
	if !c.Docker.Enabled || !c.Docker.Prewarm.Enabled {
		return nil
	}
	if _, ok := c.Upstreams[c.Docker.Prewarm.Alias]; !ok {
		return oops.In("config").
			With("alias", c.Docker.Prewarm.Alias).
			Errorf("docker.prewarm.alias must reference a configured upstream")
	}
	return nil
}

func normalizeDockerRegistry(value, publicURL string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		value = publicURL
	}
	if value == "" {
		return "", nil
	}
	if strings.Contains(value, "://") {
		return dockerRegistryHostFromURL(value)
	}
	if strings.Contains(value, "/") {
		return "", oops.In("config").
			With("registry", value).
			Errorf("docker.prewarm.registry must be a registry host without path")
	}
	return value, nil
}

func dockerRegistryHostFromURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", oops.In("config").With("registry", value).Wrapf(err, "parse docker.prewarm.registry")
	}
	if parsed.Host == "" {
		return "", oops.In("config").With("registry", value).Errorf("docker.prewarm.registry host is required")
	}
	if strings.Trim(parsed.Path, "/") != "" {
		return "", oops.In("config").
			With("registry", value).
			Errorf("docker.prewarm.registry must not include a URL path")
	}
	return parsed.Host, nil
}
