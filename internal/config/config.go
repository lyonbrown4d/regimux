// Package config loads and validates regimux runtime configuration.
package config

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/arcgolabs/configx"
	formathcl "github.com/arcgolabs/configx/format/hcl"
	"github.com/go-playground/validator/v10"
	"github.com/samber/oops"
)

const envPrefix = "REGIMUX"

const (
	defaultUpstreamBlobTopN        = 3
	defaultUpstreamBlobMaxAttempts = 1
	defaultUpstreamProbeInterval   = 30 * time.Second
	defaultUpstreamProbeTimeout    = 3 * time.Second
	defaultUpstreamProbeCooldown   = 2 * time.Minute
)

func Load(ctx context.Context, path string, args ...string) (Config, error) {
	opts, err := buildLoadOptions(path, args...)
	if err != nil {
		return Config{}, err
	}
	return LoadWithOptions(ctx, opts...)
}

// LoadWithOptions loads config with explicit configx options and then applies
// regimux-specific normalization and validation.
func LoadWithOptions(ctx context.Context, options ...configx.Option) (Config, error) {
	opts := append(baseLoadOptions(), options...)
	var cfg Config
	if err := configx.LoadContext(ctx, &cfg, opts...); err != nil {
		return Config{}, oops.In("config").Wrapf(err, "load config")
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
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
		configx.WithValidator(validator.New(validator.WithRequiredStructEnabled())),
		configx.WithValidateLevel(configx.ValidateLevelStruct),
	}
}

func validateConfigPath(path string) error {
	if strings.ToLower(filepath.Ext(strings.TrimSpace(path))) != ".hcl" {
		return oops.In("config").With("path", path).Errorf("config file must use .hcl extension: %s", path)
	}
	return nil
}

func (c *Config) NormalizeAndValidate() error {
	if err := c.validateRoot(); err != nil {
		return err
	}
	if err := c.validateAuth(); err != nil {
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
	if err := c.validateScheduler(); err != nil {
		return err
	}
	if err := c.validateWorker(); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateRoot() error {
	if c == nil {
		return oops.In("config").Errorf("config is nil")
	}
	if strings.TrimSpace(c.Server.Listen) == "" {
		return oops.In("config").Errorf("server.listen is required")
	}
	if strings.TrimSpace(c.Server.PublicURL) == "" {
		return nil
	}
	if err := validateURL("server.public_url", c.Server.PublicURL); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateAuth() error {
	c.normalizeAuthDefaults()
	if c.Auth.TokenTTL < 0 {
		return oops.In("config").Errorf("auth.token_ttl cannot be negative")
	}
	if !c.Auth.Enabled {
		return nil
	}
	return c.validateEnabledAuth()
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

func (c *Config) validateEnabledAuth() error {
	if strings.TrimSpace(c.Auth.TokenSecret) == "" {
		return oops.In("config").Errorf("auth.token_secret is required when auth.enabled is true")
	}
	if len(c.Auth.Users) == 0 {
		return oops.In("config").Errorf("auth.users is required when auth.enabled is true")
	}
	for username, user := range c.Auth.Users {
		if err := validateAuthUser(strings.TrimSpace(username), user); err != nil {
			return err
		}
	}
	return nil
}

func validateAuthUser(username string, user AuthUserConfig) error {
	if username == "" {
		return oops.In("config").Errorf("auth.users key cannot be empty")
	}
	if strings.TrimSpace(user.Password) == "" && strings.TrimSpace(user.PasswordHash) == "" {
		return oops.In("config").With("username", username).Errorf("auth.users.%s.password or password_hash is required", username)
	}
	for _, repo := range user.Repositories {
		if strings.TrimSpace(repo) == "" {
			return oops.In("config").With("username", username).Errorf("auth.users.%s.repositories cannot contain empty entries", username)
		}
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
			return oops.In("config").Errorf("cache.redis.addrs is required when cache.backend is redis")
		}
	case "valkey":
		if len(c.Cache.Valkey.Addrs) == 0 {
			return oops.In("config").Errorf("cache.valkey.addrs is required when cache.backend is valkey")
		}
	default:
		return oops.In("config").With("backend", c.Cache.Backend).Errorf("unsupported cache.backend %q", c.Cache.Backend)
	}
	return nil
}

func validateCacheLimits(cache CacheConfig) error {
	checks := []struct {
		invalid bool
		err     error
	}{
		{cache.Memory.MaxItems < 0, oops.In("config").Errorf("cache.memory.max_items cannot be negative")},
		{cache.DefaultTTL < 0, oops.In("config").Errorf("cache.default_ttl cannot be negative")},
		{cache.Redis.DB < 0, oops.In("config").Errorf("cache.redis.db cannot be negative")},
		{cache.Valkey.DB < 0, oops.In("config").Errorf("cache.valkey.db cannot be negative")},
		{cache.Tags.MaxPageSize < 0, oops.In("config").Errorf("cache.tags.max_page_size cannot be negative")},
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
