// Package config loads and validates regimux runtime configuration.
package config

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	collectionset "github.com/arcgolabs/collectionx/set"
	"github.com/arcgolabs/configx"
	formathcl "github.com/arcgolabs/configx/format/hcl"
	"github.com/go-playground/validator/v10"
	"github.com/samber/oops"
)

const envPrefix = "REGIMUX"

const (
	defaultUpstreamBlobTopN      = 3
	defaultUpstreamProbeInterval = 30 * time.Second
	defaultUpstreamProbeTimeout  = 3 * time.Second
	defaultUpstreamProbeCooldown = 2 * time.Minute
)

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
		return Config{}, oops.In("config").Wrapf(err, "load config")
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
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

func normalizeUpstreamConfig(alias string, upstreamCfg UpstreamConfig) (UpstreamConfig, error) {
	if strings.TrimSpace(alias) == "" {
		return UpstreamConfig{}, oops.In("config").Errorf("upstream alias cannot be empty")
	}
	upstreamCfg.Alias = alias
	upstreamCfg.Registry = strings.TrimSpace(upstreamCfg.Registry)

	policy, policyErr := normalizeMirrorPolicy(alias, upstreamCfg.MirrorPolicy)
	if policyErr != nil {
		return UpstreamConfig{}, policyErr
	}
	upstreamCfg.MirrorPolicy = policy

	var blobErr error
	upstreamCfg.Blob, blobErr = normalizeUpstreamBlobConfig(alias, policy, upstreamCfg.Blob)
	if blobErr != nil {
		return UpstreamConfig{}, blobErr
	}
	var probeErr error
	upstreamCfg.Probe, probeErr = normalizeUpstreamProbeConfig(alias, upstreamCfg.Probe)
	if probeErr != nil {
		return UpstreamConfig{}, probeErr
	}
	if upstreamCfg.Blob.MirrorPolicy == "latency" {
		upstreamCfg.Probe.Enabled = true
	}

	if sourceErr := validateUpstreamSource(alias, upstreamCfg); sourceErr != nil {
		return UpstreamConfig{}, sourceErr
	}
	mirrors, err := normalizeMirrors(alias, upstreamCfg.Mirrors)
	if err != nil {
		return UpstreamConfig{}, err
	}
	upstreamCfg.Mirrors = mirrors
	if upstreamCfg.Auth.Type == "" {
		upstreamCfg.Auth.Type = "anonymous"
	}
	return upstreamCfg, nil
}

func normalizeMirrorPolicy(alias, policy string) (string, error) {
	policy = strings.ToLower(strings.TrimSpace(policy))
	if policy == "" || policy == "failover" {
		return "ordered", nil
	}
	switch policy {
	case "ordered", "round_robin":
		return policy, nil
	default:
		return "", oops.In("config").With("alias", alias, "mirror_policy", policy).Errorf("upstreams.%s.mirror_policy must be ordered or round_robin", alias)
	}
}

func normalizeUpstreamBlobConfig(alias, upstreamPolicy string, blobCfg UpstreamBlobConfig) (UpstreamBlobConfig, error) {
	if blobCfg.TopN < 0 {
		return UpstreamBlobConfig{}, oops.In("config").With("alias", alias).Errorf("upstreams.%s.blob.top_n cannot be negative", alias)
	}
	if blobCfg.MaxConcurrencyPerEndpoint < 0 {
		return UpstreamBlobConfig{}, oops.In("config").With("alias", alias).Errorf("upstreams.%s.blob.max_concurrency_per_endpoint cannot be negative", alias)
	}

	policy, err := normalizeBlobMirrorPolicy(alias, blobCfg.MirrorPolicy, upstreamPolicy)
	if err != nil {
		return UpstreamBlobConfig{}, err
	}
	blobCfg.MirrorPolicy = policy
	if blobCfg.TopN == 0 {
		blobCfg.TopN = defaultUpstreamBlobTopN
	}
	return blobCfg, nil
}

func normalizeBlobMirrorPolicy(alias, policy, upstreamPolicy string) (string, error) {
	policy = strings.ToLower(strings.TrimSpace(policy))
	if policy == "" {
		if upstreamPolicy == "" {
			return "ordered", nil
		}
		return upstreamPolicy, nil
	}
	switch policy {
	case "ordered", "round_robin", "latency":
		return policy, nil
	default:
		return "", oops.In("config").With("alias", alias, "blob_mirror_policy", policy).Errorf("upstreams.%s.blob.mirror_policy must be ordered, round_robin, or latency", alias)
	}
}

func normalizeUpstreamProbeConfig(alias string, probeCfg UpstreamProbeConfig) (UpstreamProbeConfig, error) {
	checks := []struct {
		invalid bool
		err     error
	}{
		{probeCfg.Interval < 0, oops.In("config").With("alias", alias).Errorf("upstreams.%s.probe.interval cannot be negative", alias)},
		{probeCfg.Timeout < 0, oops.In("config").With("alias", alias).Errorf("upstreams.%s.probe.timeout cannot be negative", alias)},
		{probeCfg.Cooldown < 0, oops.In("config").With("alias", alias).Errorf("upstreams.%s.probe.cooldown cannot be negative", alias)},
	}
	for _, check := range checks {
		if check.invalid {
			return UpstreamProbeConfig{}, check.err
		}
	}
	if probeCfg.Interval == 0 {
		probeCfg.Interval = defaultUpstreamProbeInterval
	}
	if probeCfg.Timeout == 0 {
		probeCfg.Timeout = defaultUpstreamProbeTimeout
	}
	if probeCfg.Cooldown == 0 {
		probeCfg.Cooldown = defaultUpstreamProbeCooldown
	}
	return probeCfg, nil
}

func validateUpstreamSource(alias string, upstreamCfg UpstreamConfig) error {
	if upstreamCfg.Registry == "" && len(upstreamCfg.Mirrors) == 0 {
		return oops.In("config").With("alias", alias).Errorf("upstreams.%s.registry or upstreams.%s.mirrors is required", alias, alias)
	}
	if upstreamCfg.Registry == "" {
		return nil
	}
	return validateURL("upstreams."+alias+".registry", upstreamCfg.Registry)
}

func normalizeMirrors(alias string, mirrors []string) ([]string, error) {
	for i, mirror := range mirrors {
		mirror = strings.TrimSpace(mirror)
		if err := validateURL(fmt.Sprintf("upstreams.%s.mirrors[%d]", alias, i), mirror); err != nil {
			return nil, err
		}
		mirrors[i] = mirror
	}
	return uniqueStrings(mirrors), nil
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
		return oops.In("config").Errorf("store.meta.driver must be bboltx")
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
	case "local", "memory":
	default:
		return oops.In("config").Errorf("store.object.driver must be local or memory")
	}
	if strings.TrimSpace(c.Store.Object.Path) == "" {
		c.Store.Object.Path = "data/objects"
	}
	return nil
}

func (c *Config) validateScheduler() error {
	checks := []struct {
		invalid bool
		err     error
	}{
		{c.Scheduler.LockTTL < 0, oops.In("config").Errorf("scheduler.lock_ttl cannot be negative")},
		{c.Scheduler.Cleanup.Interval < 0, oops.In("config").Errorf("scheduler.cleanup.interval cannot be negative")},
		{c.Scheduler.Cleanup.UnusedFor < 0, oops.In("config").Errorf("scheduler.cleanup.unused_for cannot be negative")},
		{c.Scheduler.Cleanup.MaxDeletes < 0, oops.In("config").Errorf("scheduler.cleanup.max_deletes cannot be negative")},
		{c.Scheduler.Prefetch.Interval < 0, oops.In("config").Errorf("scheduler.prefetch.interval cannot be negative")},
		{c.Scheduler.Prefetch.MaxRecords < 0, oops.In("config").Errorf("scheduler.prefetch.max_records cannot be negative")},
		{c.Scheduler.Prefetch.MinPullCount < 0, oops.In("config").Errorf("scheduler.prefetch.min_pull_count cannot be negative")},
		{c.Scheduler.Prefetch.TagsPageSize < 0, oops.In("config").Errorf("scheduler.prefetch.tags_page_size cannot be negative")},
		{c.Scheduler.Prefetch.MaxCandidatesPerRepo < 0, oops.In("config").Errorf("scheduler.prefetch.max_candidates_per_repo cannot be negative")},
		{c.Scheduler.Prefetch.MaxVersionDistance < 0, oops.In("config").Errorf("scheduler.prefetch.max_version_distance cannot be negative")},
	}
	for _, check := range checks {
		if check.invalid {
			return check.err
		}
	}
	return nil
}

func (c Config) OrderedUpstreams() *collectionmapping.OrderedMap[string, UpstreamConfig] {
	aliases := c.UpstreamAliases()
	out := collectionmapping.NewOrderedMapWithCapacity[string, UpstreamConfig](aliases.Len())
	aliases.Range(func(_ int, alias string) bool {
		out.Set(alias, c.Upstreams[alias])
		return true
	})
	return out
}

func (c Config) UpstreamAliases() *collectionlist.List[string] {
	return collectionlist.NewList(collectionmapping.NewMapFrom(c.Upstreams).Keys()...).
		Sort(strings.Compare)
}

func validateURL(name, value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return oops.In("config").With("name", name, "value", value).Wrapf(err, "%s is invalid", name)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return oops.In("config").With("name", name, "value", value).Errorf("%s must be an absolute URL", name)
	}
	return nil
}

func uniqueStrings(values []string) []string {
	out := collectionset.NewOrderedSetWithCapacity[string](len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		out.Add(value)
	}
	return out.Values()
}
