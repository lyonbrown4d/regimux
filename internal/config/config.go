// Package config loads and validates regimux runtime configuration.
package config

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/arcgolabs/configx"
	formathcl "github.com/arcgolabs/configx/format/hcl"
	"github.com/go-playground/validator/v10"
)

const envPrefix = "REGIMUX"

func Load(ctx context.Context, path string) (Config, error) {
	opts := []configx.Option{
		formathcl.WithHCLSupport(),
		configx.WithDefaults(defaultValues()),
		configx.WithDotenv(),
		configx.WithIgnoreDotenvError(true),
		configx.WithEnvPrefix(envPrefix),
		configx.WithEnvSeparator("__"),
		configx.WithValidator(validator.New(validator.WithRequiredStructEnabled())),
		configx.WithValidateLevel(configx.ValidateLevelStruct),
	}
	if strings.TrimSpace(path) != "" {
		if err := validateConfigPath(path); err != nil {
			return Config{}, err
		}
		opts = append(opts, configx.WithFiles(path))
	}

	var cfg Config
	if err := configx.LoadContext(ctx, &cfg, opts...); err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateConfigPath(path string) error {
	if strings.ToLower(filepath.Ext(strings.TrimSpace(path))) != ".hcl" {
		return fmt.Errorf("config file must use .hcl extension: %s", path)
	}
	return nil
}

func (c *Config) NormalizeAndValidate() error {
	if err := c.validateRoot(); err != nil {
		return err
	}
	if err := c.validateCache(); err != nil {
		return err
	}
	if err := c.normalizeUpstreams(); err != nil {
		return err
	}
	if err := c.validateStore(); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateRoot() error {
	if c == nil {
		return errors.New("config is nil")
	}
	if strings.TrimSpace(c.Server.Listen) == "" {
		return errors.New("server.listen is required")
	}
	if strings.TrimSpace(c.Server.PublicURL) == "" {
		return nil
	}
	if err := validateURL("server.public_url", c.Server.PublicURL); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateCache() error {
	if err := c.normalizeCacheBackend(); err != nil {
		return err
	}
	return validateCacheLimits(c.Cache)
}

func (c *Config) normalizeCacheBackend() error {
	c.Cache.Backend = strings.ToLower(strings.TrimSpace(c.Cache.Backend))
	switch c.Cache.Backend {
	case "", "memory":
		c.Cache.Backend = "memory"
	case "redis":
		if len(c.Cache.Redis.Addrs) == 0 {
			return errors.New("cache.redis.addrs is required when cache.backend is redis")
		}
	case "valkey":
		if len(c.Cache.Valkey.Addrs) == 0 {
			return errors.New("cache.valkey.addrs is required when cache.backend is valkey")
		}
	default:
		return fmt.Errorf("unsupported cache.backend %q", c.Cache.Backend)
	}
	return nil
}

func validateCacheLimits(cache CacheConfig) error {
	checks := []struct {
		invalid bool
		err     error
	}{
		{cache.Memory.MaxItems < 0, errors.New("cache.memory.max_items cannot be negative")},
		{cache.DefaultTTL < 0, errors.New("cache.default_ttl cannot be negative")},
		{cache.Redis.DB < 0, errors.New("cache.redis.db cannot be negative")},
		{cache.Valkey.DB < 0, errors.New("cache.valkey.db cannot be negative")},
		{cache.Tags.MaxPageSize < 0, errors.New("cache.tags.max_page_size cannot be negative")},
	}
	for _, check := range checks {
		if check.invalid {
			return check.err
		}
	}
	return nil
}

func (c *Config) normalizeUpstreams() error {
	if len(c.Upstreams) == 0 {
		return errors.New("at least one upstream is required")
	}
	for _, alias := range c.UpstreamAliases() {
		upstreamCfg, err := normalizeUpstreamConfig(alias, c.Upstreams[alias])
		if err != nil {
			return err
		}
		c.Upstreams[alias] = upstreamCfg
	}
	return nil
}

func normalizeUpstreamConfig(alias string, upstreamCfg UpstreamConfig) (UpstreamConfig, error) {
	if strings.TrimSpace(alias) == "" {
		return UpstreamConfig{}, errors.New("upstream alias cannot be empty")
	}
	upstreamCfg.Alias = alias
	upstreamCfg.Registry = strings.TrimSpace(upstreamCfg.Registry)

	policy, policyErr := normalizeMirrorPolicy(alias, upstreamCfg.MirrorPolicy)
	if policyErr != nil {
		return UpstreamConfig{}, policyErr
	}
	upstreamCfg.MirrorPolicy = policy

	if sourceErr := validateUpstreamSource(alias, upstreamCfg); sourceErr != nil {
		return UpstreamConfig{}, sourceErr
	}
	mirrors, err := normalizeMirrors(alias, upstreamCfg.Mirrors)
	if err != nil {
		return UpstreamConfig{}, err
	}
	upstreamCfg.Mirrors = mirrors
	if upstreamCfg.Auth.Type == "" {
		upstreamCfg.Auth.Type = "anonymous"
	}
	return upstreamCfg, nil
}

func normalizeMirrorPolicy(alias, policy string) (string, error) {
	policy = strings.ToLower(strings.TrimSpace(policy))
	if policy == "" || policy == "failover" {
		return "ordered", nil
	}
	switch policy {
	case "ordered", "round_robin":
		return policy, nil
	default:
		return "", fmt.Errorf("upstreams.%s.mirror_policy must be ordered or round_robin", alias)
	}
}

func validateUpstreamSource(alias string, upstreamCfg UpstreamConfig) error {
	if upstreamCfg.Registry == "" && len(upstreamCfg.Mirrors) == 0 {
		return fmt.Errorf("upstreams.%s.registry or upstreams.%s.mirrors is required", alias, alias)
	}
	if upstreamCfg.Registry == "" {
		return nil
	}
	return validateURL("upstreams."+alias+".registry", upstreamCfg.Registry)
}

func normalizeMirrors(alias string, mirrors []string) ([]string, error) {
	for i, mirror := range mirrors {
		mirror = strings.TrimSpace(mirror)
		if err := validateURL(fmt.Sprintf("upstreams.%s.mirrors[%d]", alias, i), mirror); err != nil {
			return nil, err
		}
		mirrors[i] = mirror
	}
	return uniqueStrings(mirrors), nil
}

func (c *Config) validateStore() error {
	metaDriver := strings.ToLower(strings.TrimSpace(c.Store.Meta.Driver))
	if metaDriver == "" {
		metaDriver = "bboltx"
	}
	c.Store.Meta.Driver = metaDriver
	switch metaDriver {
	case "bboltx":
	default:
		return errors.New("store.meta.driver must be bboltx")
	}
	if strings.TrimSpace(c.Store.Meta.Path) == "" {
		c.Store.Meta.Path = "data/regimux.db"
	}

	objectDriver := strings.ToLower(strings.TrimSpace(c.Store.Object.Driver))
	if objectDriver == "" {
		objectDriver = "local"
	}
	c.Store.Object.Driver = objectDriver
	switch objectDriver {
	case "local":
	default:
		return errors.New("store.object.driver must be local")
	}
	if strings.TrimSpace(c.Store.Object.Path) == "" {
		c.Store.Object.Path = "data/objects"
	}
	return nil
}

func (c Config) OrderedUpstreams() *collectionmapping.OrderedMap[string, UpstreamConfig] {
	aliases := c.UpstreamAliases()
	out := collectionmapping.NewOrderedMapWithCapacity[string, UpstreamConfig](len(aliases))
	for _, alias := range aliases {
		out.Set(alias, c.Upstreams[alias])
	}
	return out
}

func (c Config) UpstreamAliases() []string {
	aliases := make([]string, 0, len(c.Upstreams))
	for alias := range c.Upstreams {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return aliases
}

func validateURL(name, value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", name, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must be an absolute URL", name)
	}
	return nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
