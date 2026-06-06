package admin

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func configRows(cfg config.Config) *collectionlist.List[ConfigRow] {
	rows := collectionlist.NewList[ConfigRow]()
	addServerRows(rows, cfg.Server)
	addAuthRows(rows, cfg.Auth)
	addLogRows(rows, cfg.Log)
	addCacheRows(rows, cfg.Cache)
	addStoreRows(rows, cfg.Store)
	addSchedulerRows(rows, cfg.Scheduler)
	addWorkerRows(rows, cfg.Worker)
	addUpstreamRows(rows, cfg)
	return rows
}

func configSourceRows(locale string, messages *Messages) *collectionlist.List[ConfigSourceRow] {
	status := messages.Translate(locale, "value.unavailable")
	detail := messages.Translate(locale, "hint.config_sources_unavailable")
	return collectionlist.NewList(
		ConfigSourceRow{Name: "default", Status: status, Detail: detail},
		ConfigSourceRow{Name: "file", Status: status, Detail: detail},
		ConfigSourceRow{Name: "env", Status: status, Detail: detail},
		ConfigSourceRow{Name: "cli", Status: status, Detail: detail},
	)
}

func addServerRows(rows *collectionlist.List[ConfigRow], cfg config.ServerConfig) {
	addRow(rows, "server.listen", cfg.Listen)
	addRow(rows, "server.public_url", cfg.PublicURL)
	addRow(rows, "server.read_timeout", durationString(cfg.ReadTimeout))
	addRow(rows, "server.write_timeout", durationString(cfg.WriteTimeout))
	addRow(rows, "server.idle_timeout", durationString(cfg.IdleTimeout))
	addServerMiddlewareRows(rows, cfg.Middleware)
}

func addServerMiddlewareRows(rows *collectionlist.List[ConfigRow], cfg config.ServerMiddlewareConfig) {
	addRow(rows, "server.middleware.request_id.enabled", boolString(cfg.RequestID.Enabled))
	addRow(rows, "server.middleware.request_id.header", cfg.RequestID.Header)
	addRow(rows, "server.middleware.request_logger.enabled", boolString(cfg.RequestLogger.Enabled))
	addRow(rows, "server.middleware.healthcheck.enabled", boolString(cfg.Healthcheck.Enabled))
	addRow(rows, "server.middleware.healthcheck.liveness_path", cfg.Healthcheck.LivenessPath)
	addRow(rows, "server.middleware.healthcheck.readiness_path", cfg.Healthcheck.ReadinessPath)
	addRow(rows, "server.middleware.etag.enabled", boolString(cfg.ETag.Enabled))
	addRow(rows, "server.middleware.security_headers.enabled", boolString(cfg.SecurityHeaders.Enabled))
	addRow(rows, "server.middleware.compress.enabled", boolString(cfg.Compress.Enabled))
	addRow(rows, "server.middleware.compress.level", cfg.Compress.Level)
	addRow(rows, "server.middleware.rate_limit.enabled", boolString(cfg.RateLimit.Enabled))
	addRow(rows, "server.middleware.csrf.enabled", boolString(cfg.CSRF.Enabled))
	addRow(rows, "server.middleware.pprof.enabled", boolString(cfg.Pprof.Enabled))
}

func addAuthRows(rows *collectionlist.List[ConfigRow], cfg config.RegistryAuthConfig) {
	addRow(rows, "auth.enabled", boolString(cfg.Enabled))
	addRow(rows, "auth.service", cfg.Service)
	addRow(rows, "auth.realm", cfg.Realm)
	addRow(rows, "auth.issuer", cfg.Issuer)
	addRow(rows, "auth.token_secret", mask(cfg.TokenSecret))
	addRow(rows, "auth.token_ttl", durationString(cfg.TokenTTL))
	addRow(rows, "auth.users", strconv.Itoa(len(cfg.Users)))
}

func addLogRows(rows *collectionlist.List[ConfigRow], cfg config.LogConfig) {
	addRow(rows, "log.level", cfg.Level)
	addRow(rows, "log.console", boolString(cfg.Console))
	addRow(rows, "log.file", cfg.File)
	addRow(rows, "log.add_caller", boolString(cfg.AddCaller))
}

func addCacheRows(rows *collectionlist.List[ConfigRow], cfg config.CacheConfig) {
	addRow(rows, "cache.backend", cfg.Backend)
	addRow(rows, "cache.prefix", cfg.Prefix)
	addRow(rows, "cache.default_ttl", durationString(cfg.DefaultTTL))
	addRow(rows, "cache.memory.max_items", strconv.Itoa(cfg.Memory.MaxItems))
	addExternalCacheRows(rows, "cache.redis", cfg.Redis)
	addExternalCacheRows(rows, "cache.valkey", cfg.Valkey)
	addRow(rows, "cache.manifest.tag_ttl", durationString(cfg.Manifest.TagTTL))
	addRow(rows, "cache.manifest.stale_if_error", boolString(cfg.Manifest.StaleIfError))
	addRow(rows, "cache.blob.stream_and_cache", boolString(cfg.Blob.StreamAndCache))
	addRow(rows, "cache.tags.ttl", durationString(cfg.Tags.TTL))
	addRow(rows, "cache.referrers.ttl", durationString(cfg.Referrers.TTL))
}

func addExternalCacheRows(rows *collectionlist.List[ConfigRow], prefix string, cfg config.ExternalCacheConfig) {
	addRow(rows, prefix+".addrs", fmt.Sprintf("%v", cfg.Addrs))
	addRow(rows, prefix+".username", cfg.Username)
	addRow(rows, prefix+".password", mask(cfg.Password))
	addRow(rows, prefix+".db", strconv.Itoa(cfg.DB))
}

func addStoreRows(rows *collectionlist.List[ConfigRow], cfg config.StoreConfig) {
	addRow(rows, "store.meta.driver", cfg.Meta.Driver)
	addRow(rows, "store.meta.path", cfg.Meta.Path)
	addRow(rows, "store.object.driver", cfg.Object.Driver)
	addRow(rows, "store.object.path", cfg.Object.Path)
}

func addSchedulerRows(rows *collectionlist.List[ConfigRow], cfg config.SchedulerConfig) {
	addRow(rows, "scheduler.enabled", boolString(cfg.Enabled))
	addRow(rows, "scheduler.distributed_lock", boolString(cfg.DistributedLock))
	addRow(rows, "scheduler.lock_ttl", durationString(cfg.LockTTL))
	addRow(rows, "scheduler.cleanup.enabled", boolString(cfg.Cleanup.Enabled))
	addRow(rows, "scheduler.cleanup.interval", durationString(cfg.Cleanup.Interval))
	addRow(rows, "scheduler.cleanup.unused_for", durationString(cfg.Cleanup.UnusedFor))
	addRow(rows, "scheduler.cleanup.max_scan", strconv.Itoa(cfg.Cleanup.MaxScan))
	addRow(rows, "scheduler.cleanup.max_deletes", strconv.Itoa(cfg.Cleanup.MaxDeletes))
	addRow(rows, "scheduler.cleanup.max_bytes", int64String(cfg.Cleanup.MaxBytes))
	addRow(rows, "scheduler.cleanup.target_bytes", int64String(cfg.Cleanup.TargetBytes))
	addRow(rows, "scheduler.cleanup.dry_run", boolString(cfg.Cleanup.DryRun))
	addRow(rows, "scheduler.cleanup.distributed", boolString(cfg.Cleanup.Distributed))
	addRow(rows, "scheduler.prefetch.enabled", boolString(cfg.Prefetch.Enabled))
	addRow(rows, "scheduler.prefetch.interval", durationString(cfg.Prefetch.Interval))
	addRow(rows, "scheduler.prefetch.min_pull_count", strconv.FormatInt(cfg.Prefetch.MinPullCount, 10))
	addRow(rows, "scheduler.prefetch.max_records", strconv.Itoa(cfg.Prefetch.MaxRecords))
	addRow(rows, "scheduler.prefetch.tags_page_size", strconv.Itoa(cfg.Prefetch.TagsPageSize))
	addRow(rows, "scheduler.prefetch.max_candidates_per_repo", strconv.Itoa(cfg.Prefetch.MaxCandidatesPerRepo))
	addRow(rows, "scheduler.prefetch.max_version_distance", strconv.Itoa(cfg.Prefetch.MaxVersionDistance))
	addRow(rows, "scheduler.prefetch.max_bytes", int64String(cfg.Prefetch.MaxBytes))
	addRow(rows, "scheduler.prefetch.max_tasks", strconv.Itoa(cfg.Prefetch.MaxTasks))
	addRow(rows, "scheduler.prefetch.max_repositories", strconv.Itoa(cfg.Prefetch.MaxRepositories))
	addRow(rows, "scheduler.prefetch.failure_backoff", durationString(cfg.Prefetch.FailureBackoff))
	addRow(rows, "scheduler.prefetch.retry_window", durationString(cfg.Prefetch.RetryWindow))
	addRow(rows, "scheduler.prefetch.distributed", boolString(cfg.Prefetch.Distributed))
	addRow(rows, "scheduler.manifest_refresh.enabled", boolString(cfg.ManifestRefresh.Enabled))
	addRow(rows, "scheduler.manifest_refresh.interval", durationString(cfg.ManifestRefresh.Interval))
	addRow(rows, "scheduler.manifest_refresh.distributed", boolString(cfg.ManifestRefresh.Distributed))
	addManifestRefreshEcosystemRows(rows, cfg.ManifestRefresh)
}

func addManifestRefreshEcosystemRows(rows *collectionlist.List[ConfigRow], cfg config.SchedulerManifestRefreshConfig) {
	if len(cfg.Ecosystems) == 0 {
		return
	}
	ecosystems := collectionlist.NewListWithCapacity[string](len(cfg.Ecosystems))
	for ecosystemName := range cfg.Ecosystems {
		ecosystems.Add(ecosystemName)
	}
	ecosystems.Sort(strings.Compare).Range(func(_ int, ecosystemName string) bool {
		prefix := "scheduler.manifest_refresh.ecosystems." + ecosystemName
		effective := cfg.EffectiveFor(ecosystemName)
		addRow(rows, prefix+".enabled", boolString(effective.Enabled))
		addRow(rows, prefix+".interval", durationString(effective.Interval))
		addRow(rows, prefix+".distributed", boolString(effective.Distributed))
		return true
	})
}

func addWorkerRows(rows *collectionlist.List[ConfigRow], cfg config.WorkerConfig) {
	addRow(rows, "worker.probe_concurrency", strconv.Itoa(cfg.ProbeConcurrency))
	addRow(rows, "worker.prefetch_concurrency", strconv.Itoa(cfg.PrefetchConcurrency))
}

func addUpstreamRows(rows *collectionlist.List[ConfigRow], cfg config.Config) {
	cfg.OrderedContainerUpstreams().Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		addConfigUpstreamRows(rows, "container."+alias, upstreamCfg, true)
		return true
	})
	cfg.OrderedGoUpstreams().Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		addConfigUpstreamRows(rows, "go."+alias, upstreamCfg, false)
		return true
	})
	cfg.OrderedNPMUpstreams().Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		addConfigUpstreamRows(rows, "npm."+alias, upstreamCfg, false)
		return true
	})
	cfg.OrderedPyPIUpstreams().Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		addConfigUpstreamRows(rows, "pypi."+alias, upstreamCfg, false)
		return true
	})
	cfg.OrderedMavenUpstreams().Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		addConfigUpstreamRows(rows, "maven."+alias, upstreamCfg, false)
		return true
	})
}

func addConfigUpstreamRows(rows *collectionlist.List[ConfigRow], prefix string, upstreamCfg config.UpstreamConfig, container bool) {
	addRow(rows, prefix+".registry", upstreamCfg.Registry)
	addRow(rows, prefix+".mirrors", fmt.Sprintf("%v", upstreamCfg.Mirrors))
	addRow(rows, prefix+".mirror_policy", upstreamCfg.MirrorPolicy)
	addRow(rows, prefix+".auth.type", upstreamCfg.Auth.Type)
	addRow(rows, prefix+".auth.username", upstreamCfg.Auth.Username)
	addRow(rows, prefix+".auth.password", mask(upstreamCfg.Auth.Password))
	addRow(rows, prefix+".auth.token", mask(upstreamCfg.Auth.Token))
	if !container {
		return
	}
	addRow(rows, prefix+".default_namespace", upstreamCfg.DefaultNamespace)
	addRow(rows, prefix+".blob.mirror_policy", upstreamCfg.Blob.MirrorPolicy)
	addRow(rows, prefix+".probe.enabled", boolString(upstreamCfg.Probe.Enabled))
	addRow(rows, prefix+".probe.interval", durationString(upstreamCfg.Probe.Interval))
}

func addRow(rows *collectionlist.List[ConfigRow], path, value string) {
	rows.Add(ConfigRow{Path: path, Value: dash(value)})
}

func durationString(value time.Duration) string {
	if value <= 0 {
		return "0s"
	}
	return value.String()
}

func boolString(value bool) string {
	return strconv.FormatBool(value)
}

func int64String(value int64) string {
	return strconv.FormatInt(value, 10)
}

func mask(value string) string {
	if value == "" {
		return ""
	}
	return "******"
}
