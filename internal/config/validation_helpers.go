package config

import (
	"net/url"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/samber/lo"
	"github.com/samber/oops"
)

const defaultSFTPObjectTimeout = 10 * time.Second

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
	if objectDriver != "s3" && strings.TrimSpace(c.Store.Object.Path) == "" {
		c.Store.Object.Path = "data/objects"
	}
	c.Store.Object.S3.Bucket = strings.TrimSpace(c.Store.Object.S3.Bucket)
	c.Store.Object.S3.Prefix = strings.Trim(strings.TrimSpace(c.Store.Object.S3.Prefix), "/")
	c.Store.Object.S3.Region = strings.TrimSpace(c.Store.Object.S3.Region)
	c.Store.Object.S3.Endpoint = strings.TrimSpace(c.Store.Object.S3.Endpoint)
	c.Store.Object.S3.AccessKeyID = strings.TrimSpace(c.Store.Object.S3.AccessKeyID)
	c.Store.Object.S3.SecretAccessKey = strings.TrimSpace(c.Store.Object.S3.SecretAccessKey)
	c.Store.Object.S3.SessionToken = strings.TrimSpace(c.Store.Object.S3.SessionToken)
	c.Store.Object.S3.Profile = strings.TrimSpace(c.Store.Object.S3.Profile)
	c.Store.Object.SFTP.Addr = strings.TrimSpace(c.Store.Object.SFTP.Addr)
	c.Store.Object.SFTP.Username = strings.TrimSpace(c.Store.Object.SFTP.Username)
	c.Store.Object.SFTP.Password = strings.TrimSpace(c.Store.Object.SFTP.Password)
	c.Store.Object.SFTP.PrivateKey = strings.TrimSpace(c.Store.Object.SFTP.PrivateKey)
	c.Store.Object.SFTP.PrivateKeyPassphrase = strings.TrimSpace(c.Store.Object.SFTP.PrivateKeyPassphrase)
	c.Store.Object.SFTP.KnownHostsPath = strings.TrimSpace(c.Store.Object.SFTP.KnownHostsPath)
	c.Store.Object.SFTP.HostKey = strings.TrimSpace(c.Store.Object.SFTP.HostKey)
	if objectDriver == "sftp" && c.Store.Object.SFTP.Timeout == 0 {
		c.Store.Object.SFTP.Timeout = defaultSFTPObjectTimeout
	}
}

func (c Config) OrderedContainerUpstreams() *collectionmapping.OrderedMap[string, UpstreamConfig] {
	aliases := c.ContainerAliases()
	out := collectionmapping.NewOrderedMapWithCapacity[string, UpstreamConfig](aliases.Len())
	aliases.Range(func(_ int, alias string) bool {
		if upstreamCfg, ok := c.ContainerUpstream(alias); ok {
			out.Set(alias, upstreamCfg)
		}
		return true
	})
	return out
}

func (c Config) ContainerAliases() *collectionlist.List[string] {
	return sortedConfigAliases(c.Container)
}

func (c Config) ContainerUpstream(alias string) (UpstreamConfig, bool) {
	cfg, ok := c.Container[strings.TrimSpace(alias)]
	if !ok {
		return UpstreamConfig{}, false
	}
	upstreamCfg := cfg.toUpstreamConfig()
	upstreamCfg.Alias = alias
	return upstreamCfg, true
}

func (c Config) OrderedGoUpstreams() *collectionmapping.OrderedMap[string, UpstreamConfig] {
	return orderedDependencyUpstreams(c.Go, ecosystemGo)
}

func (c Config) GoUpstream(alias string) (UpstreamConfig, bool) {
	return dependencyUpstream(c.Go, ecosystemGo, alias)
}

func (c Config) OrderedNPMUpstreams() *collectionmapping.OrderedMap[string, UpstreamConfig] {
	return orderedDependencyUpstreams(c.NPM, ecosystemNPM)
}

func (c Config) NPMUpstream(alias string) (UpstreamConfig, bool) {
	return dependencyUpstream(c.NPM, ecosystemNPM, alias)
}

func (c Config) OrderedPyPIUpstreams() *collectionmapping.OrderedMap[string, UpstreamConfig] {
	return orderedDependencyUpstreams(c.PyPI, ecosystemPyPI)
}

func (c Config) PyPIUpstream(alias string) (UpstreamConfig, bool) {
	return dependencyUpstream(c.PyPI, ecosystemPyPI, alias)
}

func (c Config) OrderedMavenUpstreams() *collectionmapping.OrderedMap[string, UpstreamConfig] {
	return orderedDependencyUpstreams(c.Maven, ecosystemMaven)
}

func (c Config) MavenUpstream(alias string) (UpstreamConfig, bool) {
	return dependencyUpstream(c.Maven, ecosystemMaven, alias)
}

func orderedDependencyUpstreams(values DependencyEcosystemConfig, ecosystem string) *collectionmapping.OrderedMap[string, UpstreamConfig] {
	aliases := sortedConfigAliases(values)
	out := collectionmapping.NewOrderedMapWithCapacity[string, UpstreamConfig](aliases.Len())
	aliases.Range(func(_ int, alias string) bool {
		if upstreamCfg, ok := dependencyUpstream(values, ecosystem, alias); ok {
			out.Set(alias, upstreamCfg)
		}
		return true
	})
	return out
}

func dependencyUpstream(values DependencyEcosystemConfig, ecosystem, alias string) (UpstreamConfig, bool) {
	alias = strings.TrimSpace(alias)
	cfg, ok := values[alias]
	if !ok {
		return UpstreamConfig{}, false
	}
	upstreamCfg := cfg.toUpstreamConfig(ecosystem)
	upstreamCfg.Alias = alias
	return upstreamCfg, true
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
	return lo.Uniq(lo.WithoutEmpty(values))
}
