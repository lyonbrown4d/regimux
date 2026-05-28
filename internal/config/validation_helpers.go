package config

import (
	"net/url"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	collectionset "github.com/arcgolabs/collectionx/set"
	"github.com/samber/oops"
)

func (c *Config) normalizeStore() {
	c.normalizeMetaStore()
	c.normalizeObjectStore()
}

func (c *Config) normalizeMetaStore() {
	metaDriver := strings.ToLower(strings.TrimSpace(c.Store.Meta.Driver))
	if metaDriver == "" {
		metaDriver = "sqlite"
	}
	if metaDriver == "postgresql" || metaDriver == "pg" {
		metaDriver = "postgres"
	}
	c.Store.Meta.Driver = metaDriver
	if metaDriver == "sqlite" && strings.TrimSpace(c.Store.Meta.Path) == "" && strings.TrimSpace(c.Store.Meta.DSN) == "" {
		c.Store.Meta.Path = "data/regimux.db"
	}
}

func (c *Config) normalizeObjectStore() {
	objectDriver := strings.ToLower(strings.TrimSpace(c.Store.Object.Driver))
	if objectDriver == "" {
		objectDriver = "local"
	}
	c.Store.Object.Driver = objectDriver
	if strings.TrimSpace(c.Store.Object.Path) == "" {
		c.Store.Object.Path = "data/objects"
	}
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
