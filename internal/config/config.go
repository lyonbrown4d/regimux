package config

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/arcgolabs/configx"
	"github.com/knadh/koanf/parsers/yaml"
)

const envPrefix = "REGIMUX"

type Config struct {
	Server    ServerConfig              `json:"server" koanf:"server" mapstructure:"server" yaml:"server"`
	Log       LogConfig                 `json:"log" koanf:"log" mapstructure:"log" yaml:"log"`
	Cache     CacheConfig               `json:"cache" koanf:"cache" mapstructure:"cache" yaml:"cache"`
	Upstreams map[string]UpstreamConfig `json:"upstreams" koanf:"upstreams" mapstructure:"upstreams" yaml:"upstreams"`
}

type ServerConfig struct {
	Listen       string        `json:"listen" koanf:"listen" mapstructure:"listen" yaml:"listen"`
	PublicURL    string        `json:"public_url" koanf:"public_url" mapstructure:"public_url" yaml:"public_url"`
	ReadTimeout  time.Duration `json:"read_timeout" koanf:"read_timeout" mapstructure:"read_timeout" yaml:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout" koanf:"write_timeout" mapstructure:"write_timeout" yaml:"write_timeout"`
	IdleTimeout  time.Duration `json:"idle_timeout" koanf:"idle_timeout" mapstructure:"idle_timeout" yaml:"idle_timeout"`
}

type LogConfig struct {
	Level      string `json:"level" koanf:"level" mapstructure:"level" yaml:"level"`
	Console    bool   `json:"console" koanf:"console" mapstructure:"console" yaml:"console"`
	NoColor    bool   `json:"no_color" koanf:"no_color" mapstructure:"no_color" yaml:"no_color"`
	File       string `json:"file" koanf:"file" mapstructure:"file" yaml:"file"`
	AddCaller  bool   `json:"add_caller" koanf:"add_caller" mapstructure:"add_caller" yaml:"add_caller"`
	MaxSizeMB  int    `json:"max_size_mb" koanf:"max_size_mb" mapstructure:"max_size_mb" yaml:"max_size_mb"`
	MaxAgeDays int    `json:"max_age_days" koanf:"max_age_days" mapstructure:"max_age_days" yaml:"max_age_days"`
	MaxBackups int    `json:"max_backups" koanf:"max_backups" mapstructure:"max_backups" yaml:"max_backups"`
	TimeFormat string `json:"time_format" koanf:"time_format" mapstructure:"time_format" yaml:"time_format"`
	SetDefault bool   `json:"set_default" koanf:"set_default" mapstructure:"set_default" yaml:"set_default"`
	LocalTime  bool   `json:"local_time" koanf:"local_time" mapstructure:"local_time" yaml:"local_time"`
	Compress   bool   `json:"compress" koanf:"compress" mapstructure:"compress" yaml:"compress"`
}

type CacheConfig struct {
	Manifest  ManifestCacheConfig `json:"manifest" koanf:"manifest" mapstructure:"manifest" yaml:"manifest"`
	Blob      BlobCacheConfig     `json:"blob" koanf:"blob" mapstructure:"blob" yaml:"blob"`
	Tags      TagsCacheConfig     `json:"tags" koanf:"tags" mapstructure:"tags" yaml:"tags"`
	Referrers ReferrersConfig     `json:"referrers" koanf:"referrers" mapstructure:"referrers" yaml:"referrers"`
}

type ManifestCacheConfig struct {
	TagTTL       time.Duration `json:"tag_ttl" koanf:"tag_ttl" mapstructure:"tag_ttl" yaml:"tag_ttl"`
	StaleIfError bool          `json:"stale_if_error" koanf:"stale_if_error" mapstructure:"stale_if_error" yaml:"stale_if_error"`
	MaxStale     time.Duration `json:"max_stale" koanf:"max_stale" mapstructure:"max_stale" yaml:"max_stale"`
}

type BlobCacheConfig struct {
	StreamAndCache bool `json:"stream_and_cache" koanf:"stream_and_cache" mapstructure:"stream_and_cache" yaml:"stream_and_cache"`
}

type TagsCacheConfig struct {
	TTL         time.Duration `json:"ttl" koanf:"ttl" mapstructure:"ttl" yaml:"ttl"`
	MaxPageSize int           `json:"max_page_size" koanf:"max_page_size" mapstructure:"max_page_size" yaml:"max_page_size"`
}

type ReferrersConfig struct {
	TTL         time.Duration `json:"ttl" koanf:"ttl" mapstructure:"ttl" yaml:"ttl"`
	FallbackTag bool          `json:"fallback_tag" koanf:"fallback_tag" mapstructure:"fallback_tag" yaml:"fallback_tag"`
}

type UpstreamConfig struct {
	Alias            string        `json:"-" koanf:"-" mapstructure:"-" yaml:"-"`
	Registry         string        `json:"registry" koanf:"registry" mapstructure:"registry" yaml:"registry"`
	DefaultNamespace string        `json:"default_namespace" koanf:"default_namespace" mapstructure:"default_namespace" yaml:"default_namespace"`
	TagTTL           time.Duration `json:"tag_ttl" koanf:"tag_ttl" mapstructure:"tag_ttl" yaml:"tag_ttl"`
	Auth             AuthConfig    `json:"auth" koanf:"auth" mapstructure:"auth" yaml:"auth"`
}

type AuthConfig struct {
	Type     string `json:"type" koanf:"type" mapstructure:"type" yaml:"type"`
	Username string `json:"username" koanf:"username" mapstructure:"username" yaml:"username"`
	Password string `json:"password" koanf:"password" mapstructure:"password" yaml:"password"`
	Token    string `json:"token" koanf:"token" mapstructure:"token" yaml:"token"`
}

func Load(ctx context.Context, path string) (Config, error) {
	opts := []configx.Option{
		configx.WithDefaults(defaultValues()),
		configx.WithEnvPrefix(envPrefix),
		configx.WithEnvSeparator("__"),
		configx.WithFileParser("yaml", yaml.Parser()),
		configx.WithFileParser("yml", yaml.Parser()),
	}
	if strings.TrimSpace(path) != "" {
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
	for alias, upstreamCfg := range c.Upstreams {
		if strings.TrimSpace(alias) == "" {
			return errors.New("upstream alias cannot be empty")
		}
		if err := validateURL("upstreams."+alias+".registry", upstreamCfg.Registry); err != nil {
			return err
		}
		upstreamCfg.Alias = alias
		if upstreamCfg.Auth.Type == "" {
			upstreamCfg.Auth.Type = "anonymous"
		}
		c.Upstreams[alias] = upstreamCfg
	}
	if c.Cache.Tags.MaxPageSize < 0 {
		return errors.New("cache.tags.max_page_size cannot be negative")
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

func defaultValues() map[string]any {
	return map[string]any{
		"server.listen":                   ":5000",
		"server.public_url":               "http://localhost:5000",
		"server.read_timeout":             30 * time.Second,
		"server.write_timeout":            0,
		"server.idle_timeout":             120 * time.Second,
		"log.level":                       "info",
		"log.console":                     true,
		"log.no_color":                    true,
		"log.add_caller":                  false,
		"log.max_size_mb":                 100,
		"log.max_age_days":                7,
		"log.max_backups":                 10,
		"log.time_format":                 "2006-01-02 15:04:05",
		"log.set_default":                 true,
		"log.local_time":                  true,
		"log.compress":                    true,
		"cache.manifest.tag_ttl":          10 * time.Minute,
		"cache.manifest.stale_if_error":   true,
		"cache.manifest.max_stale":        168 * time.Hour,
		"cache.blob.stream_and_cache":     false,
		"cache.tags.ttl":                  5 * time.Minute,
		"cache.tags.max_page_size":        1000,
		"cache.referrers.ttl":             5 * time.Minute,
		"cache.referrers.fallback_tag":    true,
		"upstreams.hub.registry":          "https://registry-1.docker.io",
		"upstreams.hub.default_namespace": "library",
		"upstreams.hub.tag_ttl":           10 * time.Minute,
		"upstreams.hub.auth.type":         "anonymous",
	}
}
