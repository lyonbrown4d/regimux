package config

import "time"

type Config struct {
	Server    ServerConfig              `json:"server"    koanf:"server"    mapstructure:"server"    validate:"required"`
	Auth      RegistryAuthConfig        `json:"auth"      koanf:"auth"      mapstructure:"auth"`
	Policy    PolicyConfig              `json:"policy"    koanf:"policy"    mapstructure:"policy"`
	Log       LogConfig                 `json:"log"       koanf:"log"       mapstructure:"log"`
	Cache     CacheConfig               `json:"cache"     koanf:"cache"     mapstructure:"cache"     validate:"required"`
	Store     StoreConfig               `json:"store"     koanf:"store"     mapstructure:"store"     validate:"required"`
	Scheduler SchedulerConfig           `json:"scheduler" koanf:"scheduler" mapstructure:"scheduler"`
	Worker    WorkerConfig              `json:"worker"    koanf:"worker"    mapstructure:"worker"`
	Docker    DockerConfig              `json:"docker"    koanf:"docker"    mapstructure:"docker"`
	Container ContainerConfig           `json:"container" koanf:"container" mapstructure:"container"`
	Go        DependencyEcosystemConfig `json:"go"        koanf:"go"        mapstructure:"go"`
	NPM       DependencyEcosystemConfig `json:"npm"       koanf:"npm"       mapstructure:"npm"`
	PyPI      DependencyEcosystemConfig `json:"pypi"      koanf:"pypi"      mapstructure:"pypi"`
	Maven     DependencyEcosystemConfig `json:"maven"     koanf:"maven"     mapstructure:"maven"`
	Dist      DistEcosystemConfig       `json:"dist"      koanf:"dist"      mapstructure:"dist"`
	Upstreams map[string]UpstreamConfig `json:"-"         koanf:"-"         mapstructure:"-"`
}

type ServerConfig struct {
	Listen       string                 `json:"listen"        koanf:"listen"        mapstructure:"listen"        validate:"required"`
	PublicURL    string                 `json:"public_url"    koanf:"public_url"    mapstructure:"public_url"    validate:"omitempty,url"`
	ReadTimeout  time.Duration          `json:"read_timeout"  koanf:"read_timeout"  mapstructure:"read_timeout"  validate:"min=0"`
	WriteTimeout time.Duration          `json:"write_timeout" koanf:"write_timeout" mapstructure:"write_timeout" validate:"min=0"`
	IdleTimeout  time.Duration          `json:"idle_timeout"  koanf:"idle_timeout"  mapstructure:"idle_timeout"  validate:"min=0"`
	Middleware   ServerMiddlewareConfig `json:"middleware"    koanf:"middleware"    mapstructure:"middleware"`
}

type ServerMiddlewareConfig struct {
	RequestID       MiddlewareRequestIDConfig       `json:"request_id"       koanf:"request_id"       mapstructure:"request_id"`
	RequestLogger   MiddlewareRequestLoggerConfig   `json:"request_logger"   koanf:"request_logger"   mapstructure:"request_logger"`
	Healthcheck     MiddlewareHealthcheckConfig     `json:"healthcheck"      koanf:"healthcheck"      mapstructure:"healthcheck"`
	ETag            MiddlewareToggleConfig          `json:"etag"             koanf:"etag"             mapstructure:"etag"`
	SecurityHeaders MiddlewareSecurityHeadersConfig `json:"security_headers" koanf:"security_headers" mapstructure:"security_headers"`
	Compress        MiddlewareCompressConfig        `json:"compress"         koanf:"compress"         mapstructure:"compress"`
	RateLimit       MiddlewareRateLimitConfig       `json:"rate_limit"       koanf:"rate_limit"       mapstructure:"rate_limit"`
	CSRF            MiddlewareCSRFConfig            `json:"csrf"             koanf:"csrf"             mapstructure:"csrf"`
	Pprof           MiddlewarePprofConfig           `json:"pprof"            koanf:"pprof"            mapstructure:"pprof"`
}

type MiddlewareToggleConfig struct {
	Enabled bool `json:"enabled" koanf:"enabled" mapstructure:"enabled"`
}

type MiddlewareRequestIDConfig struct {
	Enabled bool   `json:"enabled" koanf:"enabled" mapstructure:"enabled"`
	Header  string `json:"header"  koanf:"header"  mapstructure:"header"`
}

type MiddlewareRequestLoggerConfig struct {
	Enabled bool `json:"enabled" koanf:"enabled" mapstructure:"enabled"`
}

type MiddlewareHealthcheckConfig struct {
	Enabled       bool   `json:"enabled"        koanf:"enabled"        mapstructure:"enabled"`
	LivenessPath  string `json:"liveness_path"  koanf:"liveness_path"  mapstructure:"liveness_path"`
	ReadinessPath string `json:"readiness_path" koanf:"readiness_path" mapstructure:"readiness_path"`
}

type MiddlewareSecurityHeadersConfig struct {
	Enabled                   bool   `json:"enabled"                      koanf:"enabled"                      mapstructure:"enabled"`
	ContentSecurityPolicy     string `json:"content_security_policy"      koanf:"content_security_policy"      mapstructure:"content_security_policy"`
	CrossOriginEmbedderPolicy string `json:"cross_origin_embedder_policy" koanf:"cross_origin_embedder_policy" mapstructure:"cross_origin_embedder_policy" validate:"omitempty,oneof=unsafe-none require-corp credentialless"`
	HSTSMaxAge                int    `json:"hsts_max_age"                 koanf:"hsts_max_age"                 mapstructure:"hsts_max_age"                 validate:"min=0"`
}

type MiddlewareCompressConfig struct {
	Enabled bool   `json:"enabled" koanf:"enabled" mapstructure:"enabled"`
	Level   string `json:"level"   koanf:"level"   mapstructure:"level"   validate:"omitempty,oneof=default disabled best_speed best_compression"`
}

type MiddlewareRateLimitConfig struct {
	Enabled    bool          `json:"enabled"    koanf:"enabled"    mapstructure:"enabled"`
	Max        int           `json:"max"        koanf:"max"        mapstructure:"max"        validate:"min=0"`
	Expiration time.Duration `json:"expiration" koanf:"expiration" mapstructure:"expiration" validate:"min=0"`
}

type MiddlewareCSRFConfig struct {
	Enabled        bool          `json:"enabled"         koanf:"enabled"         mapstructure:"enabled"`
	IdleTimeout    time.Duration `json:"idle_timeout"    koanf:"idle_timeout"    mapstructure:"idle_timeout"    validate:"min=0"`
	CookieName     string        `json:"cookie_name"     koanf:"cookie_name"     mapstructure:"cookie_name"`
	CookieSecure   bool          `json:"cookie_secure"   koanf:"cookie_secure"   mapstructure:"cookie_secure"`
	TrustedOrigins []string      `json:"trusted_origins" koanf:"trusted_origins" mapstructure:"trusted_origins" validate:"dive,omitempty,url"`
}

type MiddlewarePprofConfig struct {
	Enabled bool   `json:"enabled" koanf:"enabled" mapstructure:"enabled"`
	Prefix  string `json:"prefix"  koanf:"prefix"  mapstructure:"prefix"`
}

type RegistryAuthConfig struct {
	Enabled     bool                      `json:"enabled"      koanf:"enabled"      mapstructure:"enabled"`
	Service     string                    `json:"service"      koanf:"service"      mapstructure:"service"`
	Realm       string                    `json:"realm"        koanf:"realm"        mapstructure:"realm"        validate:"omitempty,url"`
	Issuer      string                    `json:"issuer"       koanf:"issuer"       mapstructure:"issuer"`
	TokenSecret string                    `json:"token_secret" koanf:"token_secret" mapstructure:"token_secret"`
	TokenTTL    time.Duration             `json:"token_ttl"    koanf:"token_ttl"    mapstructure:"token_ttl"    validate:"min=0"`
	Users       map[string]AuthUserConfig `json:"users"        koanf:"users"        mapstructure:"users"`
}

type AuthUserConfig struct {
	Password     string   `json:"password"      koanf:"password"      mapstructure:"password"`
	PasswordHash string   `json:"password_hash" koanf:"password_hash" mapstructure:"password_hash"`
	Repositories []string `json:"repositories"  koanf:"repositories"  mapstructure:"repositories"`
	Groups       []string `json:"groups"        koanf:"groups"        mapstructure:"groups"`
}

type LogConfig struct {
	Level      string `json:"level"        koanf:"level"        mapstructure:"level"        validate:"omitempty,oneof=trace debug info warn error fatal panic disabled"`
	Debug      bool   `json:"debug"        koanf:"debug"        mapstructure:"debug"`
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
	// VerifyTTL controls how often shared blobs are re-verified against upstream.
	VerifyTTL      time.Duration        `json:"verify_ttl"       koanf:"verify_ttl"       mapstructure:"verify_ttl"       validate:"min=0"`
	StreamAndCache bool                 `json:"stream_and_cache" koanf:"stream_and_cache" mapstructure:"stream_and_cache"`
	SmallCache     SmallBlobCacheConfig `json:"small_cache"      koanf:"small_cache"      mapstructure:"small_cache"`
}

type SmallBlobCacheConfig struct {
	Enabled      bool          `json:"enabled"        koanf:"enabled"        mapstructure:"enabled"`
	MaxSizeBytes int64         `json:"max_size_bytes" koanf:"max_size_bytes" mapstructure:"max_size_bytes" validate:"min=0"`
	TTL          time.Duration `json:"ttl"            koanf:"ttl"            mapstructure:"ttl"            validate:"min=0"`
}

type TagsCacheConfig struct {
	TTL         time.Duration `json:"ttl"           koanf:"ttl"           mapstructure:"ttl"           validate:"min=0"`
	MaxPageSize int           `json:"max_page_size" koanf:"max_page_size" mapstructure:"max_page_size" validate:"min=0"`
}

type ReferrersConfig struct {
	TTL         time.Duration `json:"ttl"          koanf:"ttl"          mapstructure:"ttl"          validate:"min=0"`
	FallbackTag bool          `json:"fallback_tag" koanf:"fallback_tag" mapstructure:"fallback_tag"`
}
