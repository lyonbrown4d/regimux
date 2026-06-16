package config

import "time"

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
	Driver string                `json:"driver" koanf:"driver" mapstructure:"driver" validate:"omitempty,oneof=local memory s3 sftp"`
	Path   string                `json:"path"   koanf:"path"   mapstructure:"path"`
	S3     StoreObjectS3Config   `json:"s3"     koanf:"s3"     mapstructure:"s3"`
	SFTP   StoreObjectSFTPConfig `json:"sftp"   koanf:"sftp"   mapstructure:"sftp"`
}

type StoreObjectS3Config struct {
	Bucket          string `json:"bucket"            koanf:"bucket"            mapstructure:"bucket"`
	Prefix          string `json:"prefix"            koanf:"prefix"            mapstructure:"prefix"`
	Region          string `json:"region"            koanf:"region"            mapstructure:"region"`
	Endpoint        string `json:"endpoint"          koanf:"endpoint"          mapstructure:"endpoint"          validate:"omitempty,url"`
	AccessKeyID     string `json:"access_key_id"     koanf:"access_key_id"     mapstructure:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key" koanf:"secret_access_key" mapstructure:"secret_access_key"`
	SessionToken    string `json:"session_token"     koanf:"session_token"     mapstructure:"session_token"`
	Profile         string `json:"profile"           koanf:"profile"           mapstructure:"profile"`
	ForcePathStyle  bool   `json:"force_path_style"  koanf:"force_path_style"  mapstructure:"force_path_style"`
}

type StoreObjectSFTPConfig struct {
	Addr                 string        `json:"addr"                   koanf:"addr"                   mapstructure:"addr"`
	Username             string        `json:"username"               koanf:"username"               mapstructure:"username"`
	Password             string        `json:"password"               koanf:"password"               mapstructure:"password"`
	PrivateKey           string        `json:"private_key"            koanf:"private_key"            mapstructure:"private_key"`
	PrivateKeyPassphrase string        `json:"private_key_passphrase" koanf:"private_key_passphrase" mapstructure:"private_key_passphrase"`
	KnownHostsPath       string        `json:"known_hosts_path"       koanf:"known_hosts_path"       mapstructure:"known_hosts_path"`
	HostKey              string        `json:"host_key"               koanf:"host_key"               mapstructure:"host_key"`
	Timeout              time.Duration `json:"timeout"                koanf:"timeout"                mapstructure:"timeout"                validate:"min=0"`
}

type SchedulerConfig struct {
	Enabled         bool                           `json:"enabled"          koanf:"enabled"          mapstructure:"enabled"`
	DistributedLock bool                           `json:"distributed_lock" koanf:"distributed_lock" mapstructure:"distributed_lock"`
	LockTTL         time.Duration                  `json:"lock_ttl"         koanf:"lock_ttl"         mapstructure:"lock_ttl"         validate:"min=0"`
	Cleanup         SchedulerCleanupConfig         `json:"cleanup"          koanf:"cleanup"          mapstructure:"cleanup"`
	Prefetch        SchedulerPrefetchConfig        `json:"prefetch"         koanf:"prefetch"         mapstructure:"prefetch"`
	ManifestRefresh SchedulerManifestRefreshConfig `json:"manifest_refresh" koanf:"manifest_refresh" mapstructure:"manifest_refresh"`
	Refresh         SchedulerRefreshConfig         `json:"refresh"          koanf:"refresh"          mapstructure:"refresh"`
}

type SchedulerCleanupConfig struct {
	Enabled     bool          `json:"enabled"      koanf:"enabled"      mapstructure:"enabled"`
	Interval    time.Duration `json:"interval"     koanf:"interval"     mapstructure:"interval"     validate:"min=0"`
	MaxScan     int           `json:"max_scan"     koanf:"max_scan"     mapstructure:"max_scan"     validate:"min=0"`
	UnusedFor   time.Duration `json:"unused_for"   koanf:"unused_for"   mapstructure:"unused_for"   validate:"min=0"`
	MaxDeletes  int           `json:"max_deletes"  koanf:"max_deletes"  mapstructure:"max_deletes"  validate:"min=0"`
	MaxBytes    int64         `json:"max_bytes"    koanf:"max_bytes"    mapstructure:"max_bytes"    validate:"min=0"`
	TargetBytes int64         `json:"target_bytes" koanf:"target_bytes" mapstructure:"target_bytes" validate:"min=0"`
	DryRun      bool          `json:"dry_run"      koanf:"dry_run"      mapstructure:"dry_run"`
	Distributed bool          `json:"distributed"  koanf:"distributed"  mapstructure:"distributed"`
}

type SchedulerPrefetchConfig struct {
	Enabled              bool          `json:"enabled"                 koanf:"enabled"                 mapstructure:"enabled"`
	Interval             time.Duration `json:"interval"                koanf:"interval"                mapstructure:"interval"                validate:"min=0"`
	MaxRecords           int           `json:"max_records"             koanf:"max_records"             mapstructure:"max_records"             validate:"min=0"`
	MinPullCount         int64         `json:"min_pull_count"          koanf:"min_pull_count"          mapstructure:"min_pull_count"          validate:"min=0"`
	TagsPageSize         int           `json:"tags_page_size"          koanf:"tags_page_size"          mapstructure:"tags_page_size"          validate:"min=0"`
	MaxCandidatesPerRepo int           `json:"max_candidates_per_repo" koanf:"max_candidates_per_repo" mapstructure:"max_candidates_per_repo" validate:"min=0"`
	MaxVersionDistance   int           `json:"max_version_distance"    koanf:"max_version_distance"    mapstructure:"max_version_distance"    validate:"min=0"`
	MaxBytes             int64         `json:"max_bytes"               koanf:"max_bytes"               mapstructure:"max_bytes"               validate:"min=0"`
	MaxTasks             int           `json:"max_tasks"               koanf:"max_tasks"               mapstructure:"max_tasks"               validate:"min=0"`
	MaxRepositories      int           `json:"max_repositories"        koanf:"max_repositories"        mapstructure:"max_repositories"        validate:"min=0"`
	FailureBackoff       time.Duration `json:"failure_backoff"         koanf:"failure_backoff"         mapstructure:"failure_backoff"         validate:"min=0"`
	RetryWindow          time.Duration `json:"retry_window"            koanf:"retry_window"            mapstructure:"retry_window"            validate:"min=0"`
	Accept               string        `json:"accept"                  koanf:"accept"                  mapstructure:"accept"`
	Distributed          bool          `json:"distributed"             koanf:"distributed"             mapstructure:"distributed"`
}

type SchedulerManifestRefreshConfig struct {
	Enabled     bool                                       `json:"enabled"     koanf:"enabled"     mapstructure:"enabled"`
	Interval    time.Duration                              `json:"interval"    koanf:"interval"    mapstructure:"interval"    validate:"min=0"`
	Distributed bool                                       `json:"distributed" koanf:"distributed" mapstructure:"distributed"`
	Ecosystems  map[string]SchedulerEcosystemRefreshConfig `json:"ecosystems"  koanf:"ecosystems"  mapstructure:"ecosystems"`
}

type SchedulerEcosystemRefreshConfig struct {
	Enabled     *bool         `json:"enabled"     koanf:"enabled"     mapstructure:"enabled"`
	Interval    time.Duration `json:"interval"    koanf:"interval"    mapstructure:"interval"    validate:"min=0"`
	Distributed *bool         `json:"distributed" koanf:"distributed" mapstructure:"distributed"`
}

type SchedulerRefreshConfig struct {
	Enabled     bool          `json:"enabled"     koanf:"enabled"     mapstructure:"enabled"`
	Window      time.Duration `json:"window"      koanf:"window"      mapstructure:"window"      validate:"min=0"`
	Distributed bool          `json:"distributed" koanf:"distributed" mapstructure:"distributed"`
}

type WorkerConfig struct {
	ProbeConcurrency    int `json:"probe_concurrency"    koanf:"probe_concurrency"    mapstructure:"probe_concurrency"    validate:"min=0"`
	PrefetchConcurrency int `json:"prefetch_concurrency" koanf:"prefetch_concurrency" mapstructure:"prefetch_concurrency" validate:"min=0"`
	LeaseConcurrency    int `json:"lease_concurrency"    koanf:"lease_concurrency"    mapstructure:"lease_concurrency"    validate:"min=0"`
}

type ContainerConfig map[string]ContainerRegistryConfig

type ContainerRegistryConfig struct {
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

type DependencyEcosystemConfig map[string]DependencyUpstreamConfig

type DistEcosystemConfig map[string]DistUpstreamConfig

type DependencyUpstreamConfig struct {
	Registry     string              `json:"registry"      koanf:"registry"      mapstructure:"registry"      validate:"omitempty,url"`
	Mirrors      []string            `json:"mirrors"       koanf:"mirrors"       mapstructure:"mirrors"       validate:"dive,required,url"`
	MirrorPolicy string              `json:"mirror_policy" koanf:"mirror_policy" mapstructure:"mirror_policy" validate:"omitempty,oneof=ordered failover round_robin"`
	TagTTL       time.Duration       `json:"tag_ttl"       koanf:"tag_ttl"       mapstructure:"tag_ttl"       validate:"min=0"`
	Probe        UpstreamProbeConfig `json:"probe"         koanf:"probe"         mapstructure:"probe"`
	Auth         AuthConfig          `json:"auth"          koanf:"auth"          mapstructure:"auth"`
	HTTP         HTTPConfig          `json:"http"          koanf:"http"          mapstructure:"http"`
}

type DistUpstreamConfig struct {
	Registry     string              `json:"registry"      koanf:"registry"      mapstructure:"registry"      validate:"omitempty,url"`
	Mirrors      []string            `json:"mirrors"       koanf:"mirrors"       mapstructure:"mirrors"       validate:"dive,required,url"`
	MirrorPolicy string              `json:"mirror_policy" koanf:"mirror_policy" mapstructure:"mirror_policy" validate:"omitempty,oneof=ordered failover round_robin"`
	TagTTL       time.Duration       `json:"tag_ttl"       koanf:"tag_ttl"       mapstructure:"tag_ttl"       validate:"min=0"`
	Allow        []string            `json:"allow"         koanf:"allow"         mapstructure:"allow"         validate:"dive,required"`
	Probe        UpstreamProbeConfig `json:"probe"         koanf:"probe"         mapstructure:"probe"`
	Auth         AuthConfig          `json:"auth"          koanf:"auth"          mapstructure:"auth"`
	HTTP         HTTPConfig          `json:"http"          koanf:"http"          mapstructure:"http"`
}

type UpstreamConfig struct {
	Alias            string              `json:"-"                 koanf:"-"                 mapstructure:"-"`
	Type             string              `json:"type"              koanf:"type"              mapstructure:"type"              validate:"omitempty,oneof=oci go maven pypi npm dist"`
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
	Jitter   time.Duration `json:"jitter"   koanf:"jitter"   mapstructure:"jitter"   validate:"min=0"`
}

type AuthConfig struct {
	Type     string `json:"type"     koanf:"type"     mapstructure:"type"     validate:"omitempty,oneof=anonymous basic bearer dockerhub"`
	Username string `json:"username" koanf:"username" mapstructure:"username"`
	Password string `json:"password" koanf:"password" mapstructure:"password"`
	Token    string `json:"token"    koanf:"token"    mapstructure:"token"`
}
