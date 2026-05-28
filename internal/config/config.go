// Package config loads and validates regimux runtime configuration.
package config

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/arcgolabs/configx"
	formathcl "github.com/arcgolabs/configx/format/hcl"
	"github.com/samber/oops"
)

const envPrefix = "REGIMUX"

const (
	defaultUpstreamBlobTopN        = 3
	defaultUpstreamBlobMaxAttempts = 1
	defaultUpstreamProbeInterval   = 30 * time.Second
	defaultUpstreamProbeTimeout    = 3 * time.Second
	defaultUpstreamProbeCooldown   = 2 * time.Minute
	defaultUpstreamProbeJitter     = 5 * time.Second
)

func Load(ctx context.Context, path string, args ...string) (Config, error) {
	opts, err := buildLoadOptions(path, args...)
	if err != nil {
		return Config{}, err
	}
	return LoadWithOptions(ctx, opts...)
}

// LoadWithOptions loads config with explicit configx options, applies
// regimux-specific normalization, then validates through configx's validator.
func LoadWithOptions(ctx context.Context, options ...configx.Option) (Config, error) {
	opts := append(baseLoadOptions(), options...)
	// Use the typed loader for source/default decoding, but keep validation
	// after regimux normalization to preserve existing semantics.
	loaded, err := configx.NewT[Config](opts...).LoadConfigContext(ctx)
	if err != nil {
		return Config{}, oops.In("config").Wrapf(err, "load config")
	}
	var cfg Config
	if err := loaded.Unmarshal("", &cfg); err != nil {
		return Config{}, oops.In("config").Wrapf(err, "unmarshal config")
	}
	if err := cfg.Normalize(); err != nil {
		return Config{}, err
	}
	if err := loaded.Validate(&cfg); err != nil {
		return Config{}, oops.In("config").Wrapf(err, "validate config")
	}
	return cfg, nil
}

func (c *Config) NormalizeAndValidate() error {
	if err := c.Normalize(); err != nil {
		return err
	}
	return validateConfig(c)
}

func buildLoadOptions(path string, args ...string) ([]configx.Option, error) {
	opts := []configx.Option{}
	if strings.TrimSpace(path) != "" {
		if err := validateConfigPath(path); err != nil {
			return nil, err
		}
		opts = append(opts, configx.WithFiles(path))
	}
	if len(args) > 0 {
		opts = append(opts, configx.WithArgs(args...))
	}
	return opts, nil
}

func baseLoadOptions() []configx.Option {
	return []configx.Option{
		formathcl.WithHCLSupport(),
		configx.WithTypedDefaults(defaultConfig()),
		configx.WithDotenv(),
		configx.WithIgnoreDotenvError(true),
		configx.WithEnvPrefix(envPrefix),
		configx.WithEnvSeparator("__"),
		configx.WithValidator(newConfigValidator()),
		configx.WithValidateLevel(configx.ValidateLevelStruct),
	}
}

func validateConfigPath(path string) error {
	if strings.ToLower(filepath.Ext(strings.TrimSpace(path))) != ".hcl" {
		return oops.In("config").With("path", path).Errorf("config file must use .hcl extension: %s", path)
	}
	return nil
}

func (c *Config) Normalize() error {
	if c == nil {
		return oops.In("config").Errorf("config is nil")
	}
	c.normalizeAuthDefaults()
	c.normalizeCache()
	if err := c.normalizeUpstreams(); err != nil {
		return err
	}
	c.normalizeStore()
	return nil
}

func (c *Config) normalizeAuthDefaults() {
	c.Auth.Service = strings.TrimSpace(c.Auth.Service)
	if c.Auth.Service == "" {
		c.Auth.Service = "regimux"
	}
	c.Auth.Realm = strings.TrimSpace(c.Auth.Realm)
	c.Auth.Issuer = strings.TrimSpace(c.Auth.Issuer)
	if c.Auth.Issuer == "" {
		c.Auth.Issuer = c.Auth.Service
	}
	if c.Auth.TokenTTL == 0 {
		c.Auth.TokenTTL = 15 * time.Minute
	}
}

func (c *Config) normalizeCache() {
	c.Cache.Backend = strings.ToLower(strings.TrimSpace(c.Cache.Backend))
	if c.Cache.Backend == "" {
		c.Cache.Backend = "memory"
	}
}

func (c *Config) normalizeUpstreams() error {
	if len(c.Upstreams) == 0 {
		return oops.In("config").Errorf("at least one upstream is required")
	}
	var normalizeErr error
	c.UpstreamAliases().Range(func(_ int, alias string) bool {
		upstreamCfg, err := normalizeUpstreamConfig(alias, c.Upstreams[alias])
		if err != nil {
			normalizeErr = err
			return false
		}
		c.Upstreams[alias] = upstreamCfg
		return true
	})
	if normalizeErr != nil {
		return normalizeErr
	}
	return nil
}
