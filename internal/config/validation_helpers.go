package config

import (
	"net/url"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	collectionset "github.com/arcgolabs/collectionx/set"
	"github.com/samber/oops"
)

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
		{c.Scheduler.Cleanup.MaxScan < 0, oops.In("config").Errorf("scheduler.cleanup.max_scan cannot be negative")},
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

func (c *Config) validateWorker() error {
	checks := []struct {
		invalid bool
		err     error
	}{
		{c.Worker.ProbeConcurrency < 0, oops.In("config").Errorf("worker.probe_concurrency cannot be negative")},
		{c.Worker.PrefetchConcurrency < 0, oops.In("config").Errorf("worker.prefetch_concurrency cannot be negative")},
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
