package config

import "time"

type Config struct {
	Server    ServerConfig              `json:"server"    koanf:"server"    mapstructure:"server"    validate:"required"`
	Auth      RegistryAuthConfig        `json:"auth"      koanf:"auth"      mapstructure:"auth"`
	Log       LogConfig                 `json:"log"       koanf:"log"       mapstructure:"log"`
	Cache     CacheConfig               `json:"cache"     koanf:"cache"     mapstructure:"cache"     validate:"required"`
	Store     StoreConfig               `json:"store"     koanf:"store"     mapstructure:"store"     validate:"required"`
	Scheduler SchedulerConfig           `json:"scheduler" koanf:"scheduler" mapstructure:"scheduler"`
	Worker    WorkerConfig              `json:"worker"    koanf:"worker"    mapstructure:"worker"`
	Upstreams map[string]UpstreamConfig `json:"upstreams" koanf:"upstreams" mapstructure:"upstreams" validate:"required,min=1,dive,keys,required,endkeys,required"`
}

type ServerConfig struct {
	Listen       string        `json:"listen"        koanf:"listen"        mapstructure:"listen"        validate:"required"`
	PublicURL    string        `json:"public_url"    koanf:"public_url"    mapstructure:"public_url"    validate:"omitempty,url"`
	ReadTimeout  time.Duration `json:"read_timeout"  koanf:"read_timeout"  mapstructure:"read_timeout"  validate:"min=0"`
	WriteTimeout time.Duration `json:"write_timeout" koanf:"write_timeout" mapstructure:"write_timeout" validate:"min=0"`
	IdleTimeout  time.Duration `json:"idle_timeout"  koanf:"idle_timeout"  mapstructure:"idle_timeout"  validate:"min=0"`
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
	VerifyTTL      time.Duration `json:"verify_ttl"       koanf:"verify_ttl"       mapstructure:"verify_ttl"       validate:"min=0"`
	StreamAndCache bool          `json:"stream_and_cache" koanf:"stream_and_cache" mapstructure:"stream_and_cache"`
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
	Driver string `json:"driver" koanf:"driver" mapstructure:"driver" validate:"omitempty,oneof=sqlite mysql postgres postgresql pg"`
	DSN    string `json:"dsn"    koanf:"dsn"    mapstructure:"dsn"`
	Path   string `json:"path"   koanf:"path"   mapstructure:"path"`
}

type StoreObjectConfig struct {
	Driver string `json:"driver" koanf:"driver" mapstructure:"driver" validate:"omitempty,oneof=local memory"`
	Path   string `json:"path"   koanf:"path"   mapstructure:"path"`
}

type SchedulerConfig struct {
	Enabled         bool                    `json:"enabled"          koanf:"enabled"          mapstructure:"enabled"`
	DistributedLock bool                    `json:"distributed_lock" koanf:"distributed_lock" mapstructure:"distributed_lock"`
	LockTTL         time.Duration           `json:"lock_ttl"         koanf:"lock_ttl"         mapstructure:"lock_ttl"         validate:"min=0"`
	Cleanup         SchedulerCleanupConfig  `json:"cleanup"          koanf:"cleanup"          mapstructure:"cleanup"`
	Prefetch        SchedulerPrefetchConfig `json:"prefetch"         koanf:"prefetch"         mapstructure:"prefetch"`
}

type SchedulerCleanupConfig struct {
	Enabled     bool          `json:"enabled"     koanf:"enabled"     mapstructure:"enabled"`
	Interval    time.Duration `json:"interval"    koanf:"interval"    mapstructure:"interval"    validate:"min=0"`
	MaxScan     int           `json:"max_scan"    koanf:"max_scan"    mapstructure:"max_scan"    validate:"min=0"`
	UnusedFor   time.Duration `json:"unused_for"  koanf:"unused_for"  mapstructure:"unused_for"  validate:"min=0"`
	MaxDeletes  int           `json:"max_deletes" koanf:"max_deletes" mapstructure:"max_deletes" validate:"min=0"`
	DryRun      bool          `json:"dry_run"     koanf:"dry_run"     mapstructure:"dry_run"`
	Distributed bool          `json:"distributed" koanf:"distributed" mapstructure:"distributed"`
}

type SchedulerPrefetchConfig struct {
	Enabled              bool          `json:"enabled"                 koanf:"enabled"                 mapstructure:"enabled"`
	Interval             time.Duration `json:"interval"                koanf:"interval"                mapstructure:"interval"                validate:"min=0"`
	MaxRecords           int           `json:"max_records"             koanf:"max_records"             mapstructure:"max_records"             validate:"min=0"`
	MinPullCount         int64         `json:"min_pull_count"          koanf:"min_pull_count"          mapstructure:"min_pull_count"          validate:"min=0"`
	TagsPageSize         int           `json:"tags_page_size"          koanf:"tags_page_size"          mapstructure:"tags_page_size"          validate:"min=0"`
	MaxCandidatesPerRepo int           `json:"max_candidates_per_repo" koanf:"max_candidates_per_repo" mapstructure:"max_candidates_per_repo" validate:"min=0"`
	MaxVersionDistance   int           `json:"max_version_distance"    koanf:"max_version_distance"    mapstructure:"max_version_distance"    validate:"min=0"`
	Accept               string        `json:"accept"                  koanf:"accept"                  mapstructure:"accept"`
	Distributed          bool          `json:"distributed"             koanf:"distributed"             mapstructure:"distributed"`
}

type WorkerConfig struct {
	ProbeConcurrency    int `json:"probe_concurrency"    koanf:"probe_concurrency"    mapstructure:"probe_concurrency"    validate:"min=0"`
	PrefetchConcurrency int `json:"prefetch_concurrency" koanf:"prefetch_concurrency" mapstructure:"prefetch_concurrency" validate:"min=0"`
}

type UpstreamConfig struct {
	Alias            string              `json:"-"                 koanf:"-"                 mapstructure:"-"`
	Registry         string              `json:"registry"          koanf:"registry"          mapstructure:"registry"          validate:"omitempty,url"`
	Mirrors          []string            `json:"mirrors"           koanf:"mirrors"           mapstructure:"mirrors"           validate:"dive,required,url"`
	MirrorPolicy     string              `json:"mirror_policy"     koanf:"mirror_policy"     mapstructure:"mirror_policy"     validate:"omitempty,oneof=ordered failover round_robin"`
	DefaultNamespace string              `json:"default_namespace" koanf:"default_namespace" mapstructure:"default_namespace"`
	TagTTL           time.Duration       `json:"tag_ttl"           koanf:"tag_ttl"           mapstructure:"tag_ttl"           validate:"min=0"`
	Blob             UpstreamBlobConfig  `json:"blob"              koanf:"blob"              mapstructure:"blob"`
	Probe            UpstreamProbeConfig `json:"probe"             koanf:"probe"             mapstructure:"probe"`
	Auth             AuthConfig          `json:"auth"              koanf:"auth"              mapstructure:"auth"`
	HTTP             HTTPConfig          `json:"http"              koanf:"http"              mapstructure:"http"`
}

type UpstreamBlobConfig struct {
	MirrorPolicy              string `json:"mirror_policy"                koanf:"mirror_policy"                mapstructure:"mirror_policy"                validate:"omitempty,oneof=ordered round_robin latency"`
	TopN                      int    `json:"top_n"                        koanf:"top_n"                        mapstructure:"top_n"                        validate:"min=0"`
	MaxConcurrencyPerEndpoint int    `json:"max_concurrency_per_endpoint" koanf:"max_concurrency_per_endpoint" mapstructure:"max_concurrency_per_endpoint" validate:"min=0"`
	MaxConcurrentAttempts     int    `json:"max_concurrent_attempts"      koanf:"max_concurrent_attempts"      mapstructure:"max_concurrent_attempts"      validate:"min=0"`
}

type UpstreamProbeConfig struct {
	Enabled  bool          `json:"enabled"  koanf:"enabled"  mapstructure:"enabled"`
	Interval time.Duration `json:"interval" koanf:"interval" mapstructure:"interval" validate:"min=0"`
	Timeout  time.Duration `json:"timeout"  koanf:"timeout"  mapstructure:"timeout"  validate:"min=0"`
	Cooldown time.Duration `json:"cooldown" koanf:"cooldown" mapstructure:"cooldown" validate:"min=0"`
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
