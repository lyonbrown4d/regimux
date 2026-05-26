package config

import "time"

type Config struct {
	Server    ServerConfig              `json:"server"    koanf:"server"    mapstructure:"server"    validate:"required"`
	Log       LogConfig                 `json:"log"       koanf:"log"       mapstructure:"log"`
	Cache     CacheConfig               `json:"cache"     koanf:"cache"     mapstructure:"cache"     validate:"required"`
	Store     StoreConfig               `json:"store"     koanf:"store"     mapstructure:"store"     validate:"required"`
	Upstreams map[string]UpstreamConfig `json:"upstreams" koanf:"upstreams" mapstructure:"upstreams" validate:"required,min=1,dive,keys,required,endkeys,required"`
}

type ServerConfig struct {
	Listen       string        `json:"listen"        koanf:"listen"        mapstructure:"listen"        validate:"required"`
	PublicURL    string        `json:"public_url"    koanf:"public_url"    mapstructure:"public_url"    validate:"omitempty,url"`
	ReadTimeout  time.Duration `json:"read_timeout"  koanf:"read_timeout"  mapstructure:"read_timeout"  validate:"min=0"`
	WriteTimeout time.Duration `json:"write_timeout" koanf:"write_timeout" mapstructure:"write_timeout" validate:"min=0"`
	IdleTimeout  time.Duration `json:"idle_timeout"  koanf:"idle_timeout"  mapstructure:"idle_timeout"  validate:"min=0"`
}

type LogConfig struct {
	Level      string `json:"level"        koanf:"level"        mapstructure:"level"        validate:"omitempty,oneof=trace debug info warn error fatal panic disabled"`
	Console    bool   `json:"console"      koanf:"console"      mapstructure:"console"`
	File       string `json:"file"         koanf:"file"         mapstructure:"file"`
	AddCaller  bool   `json:"add_caller"   koanf:"add_caller"   mapstructure:"add_caller"`
	MaxSizeMB  int    `json:"max_size_mb"  koanf:"max_size_mb"  mapstructure:"max_size_mb"  validate:"min=0"`
	MaxAgeDays int    `json:"max_age_days" koanf:"max_age_days" mapstructure:"max_age_days" validate:"min=0"`
	MaxBackups int    `json:"max_backups"  koanf:"max_backups"  mapstructure:"max_backups"  validate:"min=0"`
	TimeFormat string `json:"time_format"  koanf:"time_format"  mapstructure:"time_format"`
	SetDefault bool   `json:"set_default"  koanf:"set_default"  mapstructure:"set_default"`
	LocalTime  bool   `json:"local_time"   koanf:"local_time"   mapstructure:"local_time"`
	Compress   bool   `json:"compress"     koanf:"compress"     mapstructure:"compress"`
}

type CacheConfig struct {
	Backend    string              `json:"backend"     koanf:"backend"     mapstructure:"backend"     validate:"omitempty,oneof=memory redis valkey"`
	Prefix     string              `json:"prefix"      koanf:"prefix"      mapstructure:"prefix"`
	DefaultTTL time.Duration       `json:"default_ttl" koanf:"default_ttl" mapstructure:"default_ttl" validate:"min=0"`
	Memory     MemoryCacheConfig   `json:"memory"      koanf:"memory"      mapstructure:"memory"`
	Redis      ExternalCacheConfig `json:"redis"       koanf:"redis"       mapstructure:"redis"`
	Valkey     ExternalCacheConfig `json:"valkey"      koanf:"valkey"      mapstructure:"valkey"`
	Manifest   ManifestCacheConfig `json:"manifest"    koanf:"manifest"    mapstructure:"manifest"`
	Blob       BlobCacheConfig     `json:"blob"        koanf:"blob"        mapstructure:"blob"`
	Tags       TagsCacheConfig     `json:"tags"        koanf:"tags"        mapstructure:"tags"`
	Referrers  ReferrersConfig     `json:"referrers"   koanf:"referrers"   mapstructure:"referrers"`
}

type MemoryCacheConfig struct {
	MaxItems int `json:"max_items" koanf:"max_items" mapstructure:"max_items" validate:"min=0"`
}

type ExternalCacheConfig struct {
	Addrs    []string `json:"addrs"    koanf:"addrs"    mapstructure:"addrs"    validate:"dive,required"`
	Username string   `json:"username" koanf:"username" mapstructure:"username"`
	Password string   `json:"password" koanf:"password" mapstructure:"password"`
	DB       int      `json:"db"       koanf:"db"       mapstructure:"db"       validate:"min=0"`
	Debug    bool     `json:"debug"    koanf:"debug"    mapstructure:"debug"`
}

type ManifestCacheConfig struct {
	TagTTL       time.Duration `json:"tag_ttl"        koanf:"tag_ttl"        mapstructure:"tag_ttl"        validate:"min=0"`
	StaleIfError bool          `json:"stale_if_error" koanf:"stale_if_error" mapstructure:"stale_if_error"`
	MaxStale     time.Duration `json:"max_stale"      koanf:"max_stale"      mapstructure:"max_stale"      validate:"min=0"`
}

type BlobCacheConfig struct {
	StreamAndCache bool `json:"stream_and_cache" koanf:"stream_and_cache" mapstructure:"stream_and_cache"`
}

type TagsCacheConfig struct {
	TTL         time.Duration `json:"ttl"           koanf:"ttl"           mapstructure:"ttl"           validate:"min=0"`
	MaxPageSize int           `json:"max_page_size" koanf:"max_page_size" mapstructure:"max_page_size" validate:"min=0"`
}

type ReferrersConfig struct {
	TTL         time.Duration `json:"ttl"          koanf:"ttl"          mapstructure:"ttl"          validate:"min=0"`
	FallbackTag bool          `json:"fallback_tag" koanf:"fallback_tag" mapstructure:"fallback_tag"`
}

type StoreConfig struct {
	Meta   StoreMetaConfig   `json:"meta"   koanf:"meta"   mapstructure:"meta"`
	Object StoreObjectConfig `json:"object" koanf:"object" mapstructure:"object"`
}

type StoreMetaConfig struct {
	Driver string `json:"driver" koanf:"driver" mapstructure:"driver" validate:"omitempty,oneof=bboltx"`
	Path   string `json:"path"   koanf:"path"   mapstructure:"path"`
}

type StoreObjectConfig struct {
	Driver string `json:"driver" koanf:"driver" mapstructure:"driver" validate:"omitempty,oneof=local"`
	Path   string `json:"path"   koanf:"path"   mapstructure:"path"`
}

type UpstreamConfig struct {
	Alias            string        `json:"-"                 koanf:"-"                 mapstructure:"-"`
	Registry         string        `json:"registry"          koanf:"registry"          mapstructure:"registry"          validate:"omitempty,url"`
	Mirrors          []string      `json:"mirrors"           koanf:"mirrors"           mapstructure:"mirrors"           validate:"dive,required,url"`
	MirrorPolicy     string        `json:"mirror_policy"     koanf:"mirror_policy"     mapstructure:"mirror_policy"     validate:"omitempty,oneof=ordered failover round_robin"`
	DefaultNamespace string        `json:"default_namespace" koanf:"default_namespace" mapstructure:"default_namespace"`
	TagTTL           time.Duration `json:"tag_ttl"           koanf:"tag_ttl"           mapstructure:"tag_ttl"           validate:"min=0"`
	Auth             AuthConfig    `json:"auth"              koanf:"auth"              mapstructure:"auth"`
	HTTP             HTTPConfig    `json:"http"              koanf:"http"              mapstructure:"http"`
}

type AuthConfig struct {
	Type     string `json:"type"     koanf:"type"     mapstructure:"type"     validate:"omitempty,oneof=anonymous basic bearer dockerhub"`
	Username string `json:"username" koanf:"username" mapstructure:"username"`
	Password string `json:"password" koanf:"password" mapstructure:"password"`
	Token    string `json:"token"    koanf:"token"    mapstructure:"token"`
}

type HTTPConfig struct {
	Timeout time.Duration   `json:"timeout" koanf:"timeout" mapstructure:"timeout" validate:"min=0"`
	Retry   HTTPRetryConfig `json:"retry"   koanf:"retry"   mapstructure:"retry"`
	TLS     HTTPTLSConfig   `json:"tls"     koanf:"tls"     mapstructure:"tls"`
}

type HTTPRetryConfig struct {
	Enabled    bool          `json:"enabled"     koanf:"enabled"     mapstructure:"enabled"`
	MaxRetries int           `json:"max_retries" koanf:"max_retries" mapstructure:"max_retries" validate:"min=0"`
	WaitMin    time.Duration `json:"wait_min"    koanf:"wait_min"    mapstructure:"wait_min"    validate:"min=0"`
	WaitMax    time.Duration `json:"wait_max"    koanf:"wait_max"    mapstructure:"wait_max"    validate:"min=0"`
}

type HTTPTLSConfig struct {
	Enabled            bool   `json:"enabled"              koanf:"enabled"              mapstructure:"enabled"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify" koanf:"insecure_skip_verify" mapstructure:"insecure_skip_verify"`
	ServerName         string `json:"server_name"          koanf:"server_name"          mapstructure:"server_name"`
}
