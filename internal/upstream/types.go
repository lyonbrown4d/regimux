package upstream

import (
	"context"
	"io"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/reference"
)

type AuthConfig struct {
	Type     string `json:"type" koanf:"type" yaml:"type"`
	Username string `json:"username" koanf:"username" yaml:"username"`
	Password string `json:"password" koanf:"password" yaml:"password"`
	Token    string `json:"token" koanf:"token" yaml:"token"`
}

type RemoteConfig struct {
	URL                string `json:"url" koanf:"url" yaml:"url"`
	Role               string `json:"role" koanf:"role" yaml:"role"`
	AllowTagResolution bool   `json:"allow_tag_resolution" koanf:"allow_tag_resolution" yaml:"allow_tag_resolution"`
	AllowBlobFetch     bool   `json:"allow_blob_fetch" koanf:"allow_blob_fetch" yaml:"allow_blob_fetch"`
}

type Config struct {
	Alias            string         `json:"-" koanf:"-" yaml:"-"`
	Registry         string         `json:"registry" koanf:"registry" yaml:"registry"`
	DefaultNamespace string         `json:"default_namespace" koanf:"default_namespace" yaml:"default_namespace"`
	TagTTL           string         `json:"tag_ttl" koanf:"tag_ttl" yaml:"tag_ttl"`
	Auth             AuthConfig     `json:"auth" koanf:"auth" yaml:"auth"`
	Remotes          []RemoteConfig `json:"remotes" koanf:"remotes" yaml:"remotes"`
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
