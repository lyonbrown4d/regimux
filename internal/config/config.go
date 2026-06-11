// Package config loads and validates regimux runtime configuration.
package config

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/arcgolabs/configx"
	formathcl "github.com/arcgolabs/configx/format/hcl"
	"github.com/samber/lo"
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
	defaultSmallBlobCacheMaxSize   = 4 * 1024 * 1024
	defaultSmallBlobCacheTTL       = 24 * time.Hour
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
	c.normalizeServer()
	c.normalizeLog()
	c.normalizeAuthDefaults()
	c.normalizeCache()
	c.normalizePolicy()
	c.normalizeScheduler()
	if err := c.normalizeUpstreams(); err != nil {
		return err
	}
	if err := c.normalizeDocker(); err != nil {
		return err
	}
	c.normalizeStore()
	return nil
}

func (c *Config) normalizeServer() {
	c.Server.Middleware.RequestID.Header = lo.CoalesceOrEmpty(strings.TrimSpace(c.Server.Middleware.RequestID.Header), "X-Request-ID")
	c.Server.Middleware.Healthcheck.LivenessPath = normalizeHTTPPath(c.Server.Middleware.Healthcheck.LivenessPath, "/livez")
	c.Server.Middleware.Healthcheck.ReadinessPath = normalizeHTTPPath(c.Server.Middleware.Healthcheck.ReadinessPath, "/readyz")
	c.Server.Middleware.SecurityHeaders.CrossOriginEmbedderPolicy = lo.CoalesceOrEmpty(strings.ToLower(strings.TrimSpace(c.Server.Middleware.SecurityHeaders.CrossOriginEmbedderPolicy)), "unsafe-none")
	c.Server.Middleware.Compress.Level = lo.CoalesceOrEmpty(strings.ToLower(strings.TrimSpace(c.Server.Middleware.Compress.Level)), "default")
	c.Server.Middleware.RateLimit.Expiration = lo.CoalesceOrEmpty(c.Server.Middleware.RateLimit.Expiration, time.Minute)
	c.Server.Middleware.CSRF.IdleTimeout = lo.CoalesceOrEmpty(c.Server.Middleware.CSRF.IdleTimeout, 30*time.Minute)
	c.Server.Middleware.CSRF.CookieName = lo.CoalesceOrEmpty(strings.TrimSpace(c.Server.Middleware.CSRF.CookieName), "regimux_csrf")
	c.Server.Middleware.Pprof.Prefix = strings.TrimRight(normalizeHTTPPath(c.Server.Middleware.Pprof.Prefix, ""), "/")
}

func normalizeHTTPPath(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if strings.HasPrefix(value, "/") {
		return value
	}
	return "/" + value
}

func (c *Config) normalizeAuthDefaults() {
	c.Auth.Service = lo.CoalesceOrEmpty(strings.TrimSpace(c.Auth.Service), "regimux")
	c.Auth.Realm = strings.TrimSpace(c.Auth.Realm)
	c.Auth.Issuer = lo.CoalesceOrEmpty(strings.TrimSpace(c.Auth.Issuer), c.Auth.Service)
	c.Auth.TokenTTL = lo.CoalesceOrEmpty(c.Auth.TokenTTL, 15*time.Minute)
}

func (c *Config) normalizeLog() {
	if c.Log.Debug {
		c.Log.Level = "debug"
		return
	}
	c.Log.Level = strings.ToLower(strings.TrimSpace(c.Log.Level))
}

func (c *Config) normalizeCache() {
	c.Cache.Backend = strings.ToLower(strings.TrimSpace(c.Cache.Backend))
	if c.Cache.Blob.SmallCache.Enabled {
		c.Cache.Blob.SmallCache.MaxSizeBytes = lo.CoalesceOrEmpty(c.Cache.Blob.SmallCache.MaxSizeBytes, defaultSmallBlobCacheMaxSize)
		c.Cache.Blob.SmallCache.TTL = lo.CoalesceOrEmpty(c.Cache.Blob.SmallCache.TTL, defaultSmallBlobCacheTTL)
	}
}
