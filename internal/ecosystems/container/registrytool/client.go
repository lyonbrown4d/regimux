// Package registrytool wraps higher-level OCI registry clients behind a small
// RegiMux-owned API.
package registrytool

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/samber/oops"
	"go.uber.org/multierr"
	orasremote "oras.land/oras-go/v2/registry/remote"
	orasauth "oras.land/oras-go/v2/registry/remote/auth"
)

const defaultUserAgent = "regimux/dev"

type Client struct {
	userAgent string
}

type AuthConfig struct {
	Type     string
	Username string
	Password string
	Token    string
}

type Reference struct {
	Registry   string
	Repository string
	Reference  string
	Auth       AuthConfig
	PlainHTTP  bool
	Jobs       int
	PageSize   int
}

type RepositoryRef struct {
	Registry   string
	Repository string
	Auth       AuthConfig
	PlainHTTP  bool
	PageSize   int
}

type Descriptor struct {
	MediaType string
	Digest    string
	Size      int64
}

type Manifest struct {
	Descriptor Descriptor
	Content    []byte
}

func NewClient() *Client {
	return &Client{userAgent: defaultUserAgent}
}

func (c *Client) Head(ctx context.Context, ref Reference) (Descriptor, error) {
	repo, err := c.ORASRepository(ref.repositoryRef())
	if err != nil {
		return Descriptor{}, err
	}
	desc, err := repo.Resolve(ctx, normalizeReference(ref.Reference))
	if err != nil {
		return Descriptor{}, oops.In("registrytool").Wrapf(err, "head registry reference")
	}
	return descriptorFromOCI(desc), nil
}

func (c *Client) FetchManifest(ctx context.Context, ref Reference) (Manifest, error) {
	repo, err := c.ORASRepository(ref.repositoryRef())
	if err != nil {
		return Manifest{}, err
	}
	desc, rc, err := repo.FetchReference(ctx, normalizeReference(ref.Reference))
	if err != nil {
		return Manifest{}, oops.In("registrytool").Wrapf(err, "fetch registry manifest")
	}
	content, err := readAndCloseManifest(rc)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{
		Descriptor: descriptorFromOCI(desc),
		Content:    content,
	}, nil
}

func (c *Client) ListTags(ctx context.Context, ref RepositoryRef) (*collectionlist.List[string], error) {
	repo, err := c.ORASRepository(ref)
	if err != nil {
		return nil, err
	}
	tags := collectionlist.NewList[string]()
	if err := repo.Tags(ctx, "", func(page []string) error {
		tags.Add(page...)
		return nil
	}); err != nil {
		return nil, oops.In("registrytool").Wrapf(err, "list registry tags")
	}
	return tags, nil
}

func (c *Client) ORASRepository(ref RepositoryRef) (*orasremote.Repository, error) {
	host, plainHTTP, err := normalizeRegistry(ref.Registry, ref.PlainHTTP)
	if err != nil {
		return nil, err
	}
	repository := strings.Trim(host+"/"+strings.Trim(ref.Repository, "/"), "/")
	repo, err := orasremote.NewRepository(repository)
	if err != nil {
		return nil, oops.In("registrytool").With("repository", repository).Wrapf(err, "create oras repository")
	}
	repo.PlainHTTP = plainHTTP
	repo.TagListPageSize = ref.PageSize
	repo.Client = c.orasAuthClient(host, ref.Auth)
	return repo, nil
}

func (ref Reference) repositoryRef() RepositoryRef {
	return RepositoryRef{
		Registry:   ref.Registry,
		Repository: ref.Repository,
		Auth:       ref.Auth,
		PlainHTTP:  ref.PlainHTTP,
		PageSize:   ref.PageSize,
	}
}

func (c *Client) orasAuthClient(host string, cfg AuthConfig) *orasauth.Client {
	client := &orasauth.Client{
		Client: http.DefaultClient,
		Cache:  orasauth.NewCache(),
	}
	client.SetUserAgent(c.userAgent)
	credential := orasCredential(cfg)
	if credential != orasauth.EmptyCredential {
		client.Credential = orasauth.StaticCredential(host, credential)
	}
	return client
}

func normalizeRegistry(registry string, plainHTTP bool) (string, bool, error) {
	registry = strings.TrimRight(strings.TrimSpace(registry), "/")
	if registry == "" {
		return "", false, oops.In("registrytool").Errorf("registry is required")
	}
	parsed, err := url.Parse(registry)
	if err != nil {
		return "", false, oops.In("registrytool").With("registry", registry).Wrapf(err, "parse registry")
	}
	if parsed.Scheme != "" {
		if parsed.Host == "" {
			return "", false, oops.In("registrytool").With("registry", registry).Errorf("registry host is required")
		}
		return parsed.Host, plainHTTP || strings.EqualFold(parsed.Scheme, "http"), nil
	}
	return registry, plainHTTP, nil
}

func normalizeReference(reference string) string {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return "latest"
	}
	return reference
}

func orasCredential(cfg AuthConfig) orasauth.Credential {
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "basic", "dockerhub":
		if cfg.Username != "" || cfg.Password != "" {
			return orasauth.Credential{Username: cfg.Username, Password: cfg.Password}
		}
	case "bearer":
		if cfg.Token != "" {
			return orasauth.Credential{AccessToken: cfg.Token}
		}
	}
	return orasauth.EmptyCredential
}

func readAndCloseManifest(rc io.ReadCloser) ([]byte, error) {
	content, err := io.ReadAll(rc)
	closeErr := rc.Close()
	if err != nil || closeErr != nil {
		return nil, oops.In("registrytool").Wrapf(multierr.Combine(err, closeErr), "read registry manifest")
	}
	return content, nil
}

func descriptorFromOCI(desc ocispec.Descriptor) Descriptor {
	return Descriptor{
		MediaType: desc.MediaType,
		Digest:    desc.Digest.String(),
		Size:      desc.Size,
	}
}
