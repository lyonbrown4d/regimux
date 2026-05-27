package upstream

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/reference"
)

type AuthConfig struct {
	Type     string `json:"type"     koanf:"type"     yaml:"type"`
	Username string `json:"username" koanf:"username" yaml:"username"`
	Password string `json:"password" koanf:"password" yaml:"password"`
	Token    string `json:"token"    koanf:"token"    yaml:"token"`
}

type RemoteConfig struct {
	URL                string `json:"url"                  koanf:"url"                  yaml:"url"`
	Role               string `json:"role"                 koanf:"role"                 yaml:"role"`
	AllowTagResolution bool   `json:"allow_tag_resolution" koanf:"allow_tag_resolution" yaml:"allow_tag_resolution"`
	AllowBlobFetch     bool   `json:"allow_blob_fetch"     koanf:"allow_blob_fetch"     yaml:"allow_blob_fetch"`
}

type Config struct {
	Alias            string         `json:"-"                 koanf:"-"                 yaml:"-"`
	Registry         string         `json:"registry"          koanf:"registry"          yaml:"registry"`
	Mirrors          []string       `json:"mirrors"           koanf:"mirrors"           yaml:"mirrors"`
	MirrorPolicy     string         `json:"mirror_policy"     koanf:"mirror_policy"     yaml:"mirror_policy"`
	DefaultNamespace string         `json:"default_namespace" koanf:"default_namespace" yaml:"default_namespace"`
	TagTTL           string         `json:"tag_ttl"           koanf:"tag_ttl"           yaml:"tag_ttl"`
	Blob             BlobConfig     `json:"blob"              koanf:"blob"              yaml:"blob"`
	Probe            ProbeConfig    `json:"probe"             koanf:"probe"             yaml:"probe"`
	Auth             AuthConfig     `json:"auth"              koanf:"auth"              yaml:"auth"`
	HTTP             HTTPConfig     `json:"http"              koanf:"http"              yaml:"http"`
	Remotes          []RemoteConfig `json:"remotes"           koanf:"remotes"           yaml:"remotes"`
}

type BlobConfig struct {
	MirrorPolicy              string `json:"mirror_policy"                 koanf:"mirror_policy"                 yaml:"mirror_policy"`
	TopN                      int    `json:"top_n"                         koanf:"top_n"                         yaml:"top_n"`
	MaxConcurrencyPerEndpoint int    `json:"max_concurrency_per_endpoint"  koanf:"max_concurrency_per_endpoint"  yaml:"max_concurrency_per_endpoint"`
	MaxConcurrentAttempts     int    `json:"max_concurrent_attempts"        koanf:"max_concurrent_attempts"       yaml:"max_concurrent_attempts"`
}

type ProbeConfig struct {
	Enabled  bool          `json:"enabled"  koanf:"enabled"  yaml:"enabled"`
	Interval time.Duration `json:"interval" koanf:"interval" yaml:"interval"`
	Timeout  time.Duration `json:"timeout"  koanf:"timeout"  yaml:"timeout"`
	Cooldown time.Duration `json:"cooldown" koanf:"cooldown" yaml:"cooldown"`
}

type HTTPConfig struct {
	Timeout time.Duration   `json:"timeout" koanf:"timeout" yaml:"timeout"`
	Retry   HTTPRetryConfig `json:"retry"   koanf:"retry"   yaml:"retry"`
	TLS     HTTPTLSConfig   `json:"tls"     koanf:"tls"     yaml:"tls"`
}

type HTTPRetryConfig struct {
	Enabled    bool          `json:"enabled"     koanf:"enabled"     yaml:"enabled"`
	MaxRetries int           `json:"max_retries" koanf:"max_retries" yaml:"max_retries"`
	WaitMin    time.Duration `json:"wait_min"    koanf:"wait_min"    yaml:"wait_min"`
	WaitMax    time.Duration `json:"wait_max"    koanf:"wait_max"    yaml:"wait_max"`
}

type HTTPTLSConfig struct {
	Enabled            bool   `json:"enabled"              koanf:"enabled"              yaml:"enabled"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify" koanf:"insecure_skip_verify" yaml:"insecure_skip_verify"`
	ServerName         string `json:"server_name"          koanf:"server_name"          yaml:"server_name"`
}

type RegistryClient interface {
	Ping(ctx context.Context, alias string) error
	GetManifest(ctx context.Context, req GetManifestRequest) (*ManifestResponse, error)
	GetBlob(ctx context.Context, req GetBlobRequest) (*BlobResponse, error)
	ListTags(ctx context.Context, req ListTagsRequest) (*TagsResponse, error)
	GetReferrers(ctx context.Context, req ReferrersRequest) (*ReferrersResponse, error)
}

type GetManifestRequest struct {
	UpstreamAlias string
	Repo          string
	Reference     string
	Accept        string
	Method        string
}

type ManifestResponse struct {
	Body      io.ReadCloser
	Digest    string
	MediaType string
	Size      int64
	Headers   http.Header
}

type GetBlobRequest struct {
	UpstreamAlias string
	Repo          string
	Digest        string
	Range         *reference.HTTPRange
	Method        string
}

type BlobResponse struct {
	Body       io.ReadCloser
	Digest     string
	Size       int64
	StatusCode int
	Headers    http.Header
}

type ListTagsRequest struct {
	UpstreamAlias string
	Repo          string
	N             string
	Last          string
}

type TagsResponse struct {
	Body    io.ReadCloser
	Headers http.Header
}

type ReferrersRequest struct {
	UpstreamAlias string
	Repo          string
	Digest        string
}

type ReferrersResponse struct {
	Body      io.ReadCloser
	MediaType string
	Headers   http.Header
}
