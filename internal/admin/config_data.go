package admin

import (
	"fmt"
	"strconv"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func configRows(cfg config.Config) []ConfigRow {
	rows := collectionlist.NewList[ConfigRow]()
	addServerRows(rows, cfg.Server)
	addAuthRows(rows, cfg.Auth)
	addLogRows(rows, cfg.Log)
	addCacheRows(rows, cfg.Cache)
	addStoreRows(rows, cfg.Store)
	addSchedulerRows(rows, cfg.Scheduler)
	addWorkerRows(rows, cfg.Worker)
	addUpstreamRows(rows, cfg)
	return rows.Values()
}

func configSourceRows(locale string) []ConfigSourceRow {
	status := translate(locale, "value.unavailable")
	detail := translate(locale, "hint.config_sources_unavailable")
	return []ConfigSourceRow{
		{Name: "default", Status: status, Detail: detail},
		{Name: "file", Status: status, Detail: detail},
		{Name: "env", Status: status, Detail: detail},
		{Name: "cli", Status: status, Detail: detail},
	}
}

func addServerRows(rows *collectionlist.List[ConfigRow], cfg config.ServerConfig) {
	addRow(rows, "server.listen", cfg.Listen)
	addRow(rows, "server.public_url", cfg.PublicURL)
	addRow(rows, "server.read_timeout", durationString(cfg.ReadTimeout))
	addRow(rows, "server.write_timeout", durationString(cfg.WriteTimeout))
	addRow(rows, "server.idle_timeout", durationString(cfg.IdleTimeout))
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
	addRow(rows, "scheduler.prefetch.enabled", boolString(cfg.Prefetch.Enabled))
	addRow(rows, "scheduler.prefetch.interval", durationString(cfg.Prefetch.Interval))
	addRow(rows, "scheduler.prefetch.min_pull_count", strconv.FormatInt(cfg.Prefetch.MinPullCount, 10))
}

func addWorkerRows(rows *collectionlist.List[ConfigRow], cfg config.WorkerConfig) {
	addRow(rows, "worker.probe_concurrency", strconv.Itoa(cfg.ProbeConcurrency))
	addRow(rows, "worker.prefetch_concurrency", strconv.Itoa(cfg.PrefetchConcurrency))
}

func addUpstreamRows(rows *collectionlist.List[ConfigRow], cfg config.Config) {
	cfg.OrderedUpstreams().Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		prefix := "upstreams." + alias
		addRow(rows, prefix+".registry", upstreamCfg.Registry)
		addRow(rows, prefix+".mirrors", fmt.Sprintf("%v", upstreamCfg.Mirrors))
		addRow(rows, prefix+".mirror_policy", upstreamCfg.MirrorPolicy)
		addRow(rows, prefix+".default_namespace", upstreamCfg.DefaultNamespace)
		addRow(rows, prefix+".blob.mirror_policy", upstreamCfg.Blob.MirrorPolicy)
		addRow(rows, prefix+".probe.enabled", boolString(upstreamCfg.Probe.Enabled))
		addRow(rows, prefix+".probe.interval", durationString(upstreamCfg.Probe.Interval))
		addRow(rows, prefix+".auth.type", upstreamCfg.Auth.Type)
		addRow(rows, prefix+".auth.username", upstreamCfg.Auth.Username)
		addRow(rows, prefix+".auth.password", mask(upstreamCfg.Auth.Password))
		addRow(rows, prefix+".auth.token", mask(upstreamCfg.Auth.Token))
		return true
	})
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

func mask(value string) string {
	if value == "" {
		return ""
	}
	return "******"
}
