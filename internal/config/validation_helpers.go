package config

import (
	"net/url"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	collectionset "github.com/arcgolabs/collectionx/set"
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
	clean := collectionlist.FilterMapList(collectionlist.NewList(values...), func(_ int, value string) (string, bool) {
		return value, value != ""
	})
	return collectionset.NewOrderedSetWithCapacity[string](clean.Len(), clean.Values()...).Values()
}
