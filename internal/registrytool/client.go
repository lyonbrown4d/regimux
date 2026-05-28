// Package registrytool wraps higher-level OCI registry clients behind a small
// RegiMux-owned API.
package registrytool

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/google/go-containerregistry/pkg/authn"
	gcrname "github.com/google/go-containerregistry/pkg/name"
	gcrremote "github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/opencontainers/go-digest"
	"github.com/samber/oops"
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
	parsed, opts, err := ref.containerReference(ctx, c.userAgent)
	if err != nil {
		return Descriptor{}, err
	}
	desc, err := gcrremote.Head(parsed, opts...)
	if err != nil {
		return Descriptor{}, oops.In("registrytool").Wrapf(err, "head registry reference")
	}
	return descriptorFromGCR(string(desc.MediaType), desc.Digest.String(), desc.Size), nil
}

func (c *Client) FetchManifest(ctx context.Context, ref Reference) (Manifest, error) {
	parsed, opts, err := ref.containerReference(ctx, c.userAgent)
	if err != nil {
		return Manifest{}, err
	}
	desc, err := gcrremote.Get(parsed, opts...)
	if err != nil {
		return Manifest{}, oops.In("registrytool").Wrapf(err, "fetch registry manifest")
	}
	return Manifest{
		Descriptor: descriptorFromGCR(string(desc.MediaType), desc.Digest.String(), desc.Size),
		Content:    desc.Manifest,
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

func (ref Reference) containerReference(ctx context.Context, userAgent string) (gcrname.Reference, []gcrremote.Option, error) {
	host, plainHTTP, err := normalizeRegistry(ref.Registry, ref.PlainHTTP)
	if err != nil {
		return nil, nil, err
	}
	imageRef := host + "/" + strings.Trim(ref.Repository, "/") + referenceSuffix(ref.Reference)
	nameOpts := []gcrname.Option{gcrname.WeakValidation}
	if plainHTTP {
		nameOpts = append(nameOpts, gcrname.Insecure)
	}
	parsed, err := gcrname.ParseReference(imageRef, nameOpts...)
	if err != nil {
		return nil, nil, oops.In("registrytool").With("reference", imageRef).Wrapf(err, "parse registry reference")
	}
	remoteOpts := []gcrremote.Option{
		gcrremote.WithContext(ctx),
		gcrremote.WithUserAgent(userAgent),
		gcrremote.WithAuth(containerAuthenticator(ref.Auth)),
	}
	if ref.Jobs > 0 {
		remoteOpts = append(remoteOpts, gcrremote.WithJobs(ref.Jobs))
	}
	if ref.PageSize > 0 {
		remoteOpts = append(remoteOpts, gcrremote.WithPageSize(ref.PageSize))
	}
	return parsed, remoteOpts, nil
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

func referenceSuffix(reference string) string {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		reference = "latest"
	}
	if _, err := digest.Parse(reference); err == nil {
		return "@" + reference
	}
	return ":" + reference
}

func containerAuthenticator(cfg AuthConfig) authn.Authenticator {
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "basic", "dockerhub":
		if cfg.Username != "" || cfg.Password != "" {
			return &authn.Basic{Username: cfg.Username, Password: cfg.Password}
		}
	case "bearer":
		if cfg.Token != "" {
			return &authn.Bearer{Token: cfg.Token}
		}
	}
	return authn.Anonymous
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

func descriptorFromGCR(mediaType, digestValue string, size int64) Descriptor {
	return Descriptor{MediaType: mediaType, Digest: digestValue, Size: size}
}
