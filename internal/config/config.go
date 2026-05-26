package config

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/arcgolabs/configx"
	formathcl "github.com/arcgolabs/configx/format/hcl"
	"github.com/go-playground/validator/v10"
)

const envPrefix = "REGIMUX"

type Config struct {
	Server    ServerConfig              `json:"server" koanf:"server" mapstructure:"server" validate:"required"`
	Log       LogConfig                 `json:"log" koanf:"log" mapstructure:"log"`
	Cache     CacheConfig               `json:"cache" koanf:"cache" mapstructure:"cache" validate:"required"`
	Store     StoreConfig               `json:"store" koanf:"store" mapstructure:"store" validate:"required"`
	Upstreams map[string]UpstreamConfig `json:"upstreams" koanf:"upstreams" mapstructure:"upstreams" validate:"required,min=1,dive,keys,required,endkeys,required"`
}

type ServerConfig struct {
	Listen       string        `json:"listen" koanf:"listen" mapstructure:"listen" validate:"required"`
	PublicURL    string        `json:"public_url" koanf:"public_url" mapstructure:"public_url" validate:"omitempty,url"`
	ReadTimeout  time.Duration `json:"read_timeout" koanf:"read_timeout" mapstructure:"read_timeout" validate:"min=0"`
	WriteTimeout time.Duration `json:"write_timeout" koanf:"write_timeout" mapstructure:"write_timeout" validate:"min=0"`
	IdleTimeout  time.Duration `json:"idle_timeout" koanf:"idle_timeout" mapstructure:"idle_timeout" validate:"min=0"`
}

type LogConfig struct {
	Level      string `json:"level" koanf:"level" mapstructure:"level" validate:"omitempty,oneof=trace debug info warn error fatal panic disabled"`
	Console    bool   `json:"console" koanf:"console" mapstructure:"console"`
	File       string `json:"file" koanf:"file" mapstructure:"file"`
	AddCaller  bool   `json:"add_caller" koanf:"add_caller" mapstructure:"add_caller"`
	MaxSizeMB  int    `json:"max_size_mb" koanf:"max_size_mb" mapstructure:"max_size_mb" validate:"min=0"`
	MaxAgeDays int    `json:"max_age_days" koanf:"max_age_days" mapstructure:"max_age_days" validate:"min=0"`
	MaxBackups int    `json:"max_backups" koanf:"max_backups" mapstructure:"max_backups" validate:"min=0"`
	TimeFormat string `json:"time_format" koanf:"time_format" mapstructure:"time_format"`
	SetDefault bool   `json:"set_default" koanf:"set_default" mapstructure:"set_default"`
	LocalTime  bool   `json:"local_time" koanf:"local_time" mapstructure:"local_time"`
	Compress   bool   `json:"compress" koanf:"compress" mapstructure:"compress"`
}

type CacheConfig struct {
	Backend    string              `json:"backend" koanf:"backend" mapstructure:"backend" validate:"omitempty,oneof=memory redis valkey"`
	Prefix     string              `json:"prefix" koanf:"prefix" mapstructure:"prefix"`
	DefaultTTL time.Duration       `json:"default_ttl" koanf:"default_ttl" mapstructure:"default_ttl" validate:"min=0"`
	Memory     MemoryCacheConfig   `json:"memory" koanf:"memory" mapstructure:"memory"`
	Redis      ExternalCacheConfig `json:"redis" koanf:"redis" mapstructure:"redis"`
	Valkey     ExternalCacheConfig `json:"valkey" koanf:"valkey" mapstructure:"valkey"`
	Manifest   ManifestCacheConfig `json:"manifest" koanf:"manifest" mapstructure:"manifest"`
	Blob       BlobCacheConfig     `json:"blob" koanf:"blob" mapstructure:"blob"`
	Tags       TagsCacheConfig     `json:"tags" koanf:"tags" mapstructure:"tags"`
	Referrers  ReferrersConfig     `json:"referrers" koanf:"referrers" mapstructure:"referrers"`
}

type MemoryCacheConfig struct {
	MaxItems int `json:"max_items" koanf:"max_items" mapstructure:"max_items" validate:"min=0"`
}

type ExternalCacheConfig struct {
	Addrs    []string `json:"addrs" koanf:"addrs" mapstructure:"addrs" validate:"dive,required"`
	Username string   `json:"username" koanf:"username" mapstructure:"username"`
	Password string   `json:"password" koanf:"password" mapstructure:"password"`
	DB       int      `json:"db" koanf:"db" mapstructure:"db" validate:"min=0"`
	Debug    bool     `json:"debug" koanf:"debug" mapstructure:"debug"`
}

type ManifestCacheConfig struct {
	TagTTL       time.Duration `json:"tag_ttl" koanf:"tag_ttl" mapstructure:"tag_ttl" validate:"min=0"`
	StaleIfError bool          `json:"stale_if_error" koanf:"stale_if_error" mapstructure:"stale_if_error"`
	MaxStale     time.Duration `json:"max_stale" koanf:"max_stale" mapstructure:"max_stale" validate:"min=0"`
}

type BlobCacheConfig struct {
	StreamAndCache bool `json:"stream_and_cache" koanf:"stream_and_cache" mapstructure:"stream_and_cache"`
}

type TagsCacheConfig struct {
	TTL         time.Duration `json:"ttl" koanf:"ttl" mapstructure:"ttl" validate:"min=0"`
	MaxPageSize int           `json:"max_page_size" koanf:"max_page_size" mapstructure:"max_page_size" validate:"min=0"`
}

type ReferrersConfig struct {
	TTL         time.Duration `json:"ttl" koanf:"ttl" mapstructure:"ttl" validate:"min=0"`
	FallbackTag bool          `json:"fallback_tag" koanf:"fallback_tag" mapstructure:"fallback_tag"`
}

type StoreConfig struct {
	Meta   StoreMetaConfig   `json:"meta" koanf:"meta" mapstructure:"meta"`
	Object StoreObjectConfig `json:"object" koanf:"object" mapstructure:"object"`
}

type StoreMetaConfig struct {
	Driver string `json:"driver" koanf:"driver" mapstructure:"driver" validate:"omitempty,oneof=bboltx"`
	Path   string `json:"path" koanf:"path" mapstructure:"path"`
}

type StoreObjectConfig struct {
	Driver string `json:"driver" koanf:"driver" mapstructure:"driver" validate:"omitempty,oneof=local"`
	Path   string `json:"path" koanf:"path" mapstructure:"path"`
}

type UpstreamConfig struct {
	Alias            string        `json:"-" koanf:"-" mapstructure:"-"`
	Registry         string        `json:"registry" koanf:"registry" mapstructure:"registry" validate:"omitempty,url"`
	Mirrors          []string      `json:"mirrors" koanf:"mirrors" mapstructure:"mirrors" validate:"dive,required,url"`
	MirrorPolicy     string        `json:"mirror_policy" koanf:"mirror_policy" mapstructure:"mirror_policy" validate:"omitempty,oneof=ordered failover round_robin"`
	DefaultNamespace string        `json:"default_namespace" koanf:"default_namespace" mapstructure:"default_namespace"`
	TagTTL           time.Duration `json:"tag_ttl" koanf:"tag_ttl" mapstructure:"tag_ttl" validate:"min=0"`
	Auth             AuthConfig    `json:"auth" koanf:"auth" mapstructure:"auth"`
	HTTP             HTTPConfig    `json:"http" koanf:"http" mapstructure:"http"`
}

type AuthConfig struct {
	Type     string `json:"type" koanf:"type" mapstructure:"type" validate:"omitempty,oneof=anonymous basic bearer dockerhub"`
	Username string `json:"username" koanf:"username" mapstructure:"username"`
	Password string `json:"password" koanf:"password" mapstructure:"password"`
	Token    string `json:"token" koanf:"token" mapstructure:"token"`
}

type HTTPConfig struct {
	Timeout time.Duration   `json:"timeout" koanf:"timeout" mapstructure:"timeout" validate:"min=0"`
	Retry   HTTPRetryConfig `json:"retry" koanf:"retry" mapstructure:"retry"`
	TLS     HTTPTLSConfig   `json:"tls" koanf:"tls" mapstructure:"tls"`
}

type HTTPRetryConfig struct {
	Enabled    bool          `json:"enabled" koanf:"enabled" mapstructure:"enabled"`
	MaxRetries int           `json:"max_retries" koanf:"max_retries" mapstructure:"max_retries" validate:"min=0"`
	WaitMin    time.Duration `json:"wait_min" koanf:"wait_min" mapstructure:"wait_min" validate:"min=0"`
	WaitMax    time.Duration `json:"wait_max" koanf:"wait_max" mapstructure:"wait_max" validate:"min=0"`
}

type HTTPTLSConfig struct {
	Enabled            bool   `json:"enabled" koanf:"enabled" mapstructure:"enabled"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify" koanf:"insecure_skip_verify" mapstructure:"insecure_skip_verify"`
	ServerName         string `json:"server_name" koanf:"server_name" mapstructure:"server_name"`
}

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
	if c == nil {
		return errors.New("config is nil")
	}
	if strings.TrimSpace(c.Server.Listen) == "" {
		return errors.New("server.listen is required")
	}
	if strings.TrimSpace(c.Server.PublicURL) != "" {
		if err := validateURL("server.public_url", c.Server.PublicURL); err != nil {
			return err
		}
	}
	if len(c.Upstreams) == 0 {
		return errors.New("at least one upstream is required")
	}
	if err := c.validateCache(); err != nil {
		return err
	}
	for alias, upstreamCfg := range c.Upstreams {
		if strings.TrimSpace(alias) == "" {
			return errors.New("upstream alias cannot be empty")
		}
		upstreamCfg.Registry = strings.TrimSpace(upstreamCfg.Registry)
		upstreamCfg.MirrorPolicy = strings.ToLower(strings.TrimSpace(upstreamCfg.MirrorPolicy))
		if upstreamCfg.MirrorPolicy == "" || upstreamCfg.MirrorPolicy == "failover" {
			upstreamCfg.MirrorPolicy = "ordered"
		}
		switch upstreamCfg.MirrorPolicy {
		case "ordered", "round_robin":
		default:
			return fmt.Errorf("upstreams.%s.mirror_policy must be ordered or round_robin", alias)
		}
		if upstreamCfg.Registry == "" && len(upstreamCfg.Mirrors) == 0 {
			return fmt.Errorf("upstreams.%s.registry or upstreams.%s.mirrors is required", alias, alias)
		}
		if upstreamCfg.Registry != "" {
			if err := validateURL("upstreams."+alias+".registry", upstreamCfg.Registry); err != nil {
				return err
			}
		}
		for i, mirror := range upstreamCfg.Mirrors {
			mirror = strings.TrimSpace(mirror)
			if err := validateURL(fmt.Sprintf("upstreams.%s.mirrors[%d]", alias, i), mirror); err != nil {
				return err
			}
			upstreamCfg.Mirrors[i] = mirror
		}
		upstreamCfg.Mirrors = uniqueStrings(upstreamCfg.Mirrors)
		upstreamCfg.Alias = alias
		if upstreamCfg.Auth.Type == "" {
			upstreamCfg.Auth.Type = "anonymous"
		}
		c.Upstreams[alias] = upstreamCfg
	}
	if c.Cache.Tags.MaxPageSize < 0 {
		return errors.New("cache.tags.max_page_size cannot be negative")
	}
	if err := c.validateStore(); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateCache() error {
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
	if c.Cache.Memory.MaxItems < 0 {
		return errors.New("cache.memory.max_items cannot be negative")
	}
	if c.Cache.DefaultTTL < 0 {
		return errors.New("cache.default_ttl cannot be negative")
	}
	if c.Cache.Redis.DB < 0 {
		return errors.New("cache.redis.db cannot be negative")
	}
	if c.Cache.Valkey.DB < 0 {
		return errors.New("cache.valkey.db cannot be negative")
	}
	return nil
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
		return fmt.Errorf("store.meta.driver must be bboltx")
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
		return fmt.Errorf("store.object.driver must be local")
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

func defaultValues() map[string]any {
	return map[string]any{
		"server.listen":                        ":5000",
		"server.public_url":                    "http://localhost:5000",
		"server.read_timeout":                  30 * time.Second,
		"server.write_timeout":                 0,
		"server.idle_timeout":                  120 * time.Second,
		"log.level":                            "info",
		"log.console":                          true,
		"log.add_caller":                       false,
		"log.max_size_mb":                      100,
		"log.max_age_days":                     7,
		"log.max_backups":                      10,
		"log.time_format":                      "2006-01-02 15:04:05",
		"log.set_default":                      true,
		"log.local_time":                       true,
		"log.compress":                         true,
		"cache.backend":                        "memory",
		"cache.prefix":                         "regimux",
		"cache.default_ttl":                    10 * time.Minute,
		"cache.memory.max_items":               10000,
		"cache.redis.addrs":                    []string{"127.0.0.1:6379"},
		"cache.redis.db":                       0,
		"cache.valkey.addrs":                   []string{"127.0.0.1:6379"},
		"cache.valkey.db":                      0,
		"cache.manifest.tag_ttl":               10 * time.Minute,
		"cache.manifest.stale_if_error":        true,
		"cache.manifest.max_stale":             168 * time.Hour,
		"cache.blob.stream_and_cache":          false,
		"cache.tags.ttl":                       5 * time.Minute,
		"cache.tags.max_page_size":             1000,
		"cache.referrers.ttl":                  5 * time.Minute,
		"cache.referrers.fallback_tag":         true,
		"store.meta.driver":                    "bboltx",
		"store.meta.path":                      "data/regimux.db",
		"store.object.driver":                  "local",
		"store.object.path":                    "data/objects",
		"upstreams.hub.registry":               "https://registry-1.docker.io",
		"upstreams.hub.mirror_policy":          "ordered",
		"upstreams.hub.default_namespace":      "library",
		"upstreams.hub.tag_ttl":                10 * time.Minute,
		"upstreams.hub.auth.type":              "anonymous",
		"upstreams.hub.http.timeout":           0,
		"upstreams.hub.http.retry.enabled":     true,
		"upstreams.hub.http.retry.max_retries": 2,
		"upstreams.hub.http.retry.wait_min":    100 * time.Millisecond,
		"upstreams.hub.http.retry.wait_max":    1 * time.Second,
	}
}
