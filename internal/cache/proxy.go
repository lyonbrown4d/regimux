package cache

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/upstream"
)

type Proxy struct {
	client upstream.RegistryClient
}

func NewProxy(client upstream.RegistryClient) *Proxy {
	return &Proxy{client: client}
}

func (p *Proxy) Manifests() ManifestService {
	return manifestProxy{client: p.client}
}

func (p *Proxy) Blobs() BlobService {
	return blobProxy{client: p.client}
}

func (p *Proxy) Tags() TagService {
	return tagProxy{client: p.client}
}

func (p *Proxy) Referrers() ReferrerService {
	return referrerProxy{client: p.client}
}

type manifestProxy struct {
	client upstream.RegistryClient
}

func (p manifestProxy) Get(ctx context.Context, req ManifestRequest) (*CachedManifest, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}
	resp, err := p.client.GetManifest(ctx, upstream.GetManifestRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Reference:     req.Reference,
		Accept:        req.Accept,
		Method:        req.Method,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var body []byte
	if req.Method != http.MethodHead {
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read manifest body: %w", err)
		}
	}

	return &CachedManifest{
		Digest:    resp.Digest,
		MediaType: resp.MediaType,
		Size:      resp.Size,
		Body:      body,
		Headers:   resp.Headers,
		Cache:     CacheBypass,
	}, nil
}

type blobProxy struct {
	client upstream.RegistryClient
}

func (p blobProxy) Get(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}
	resp, err := p.client.GetBlob(ctx, upstream.GetBlobRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Digest:        req.Digest,
		Range:         req.Range,
		Method:        req.Method,
	})
	if err != nil {
		return nil, err
	}

	reader := resp.Body
	if req.Method == http.MethodHead {
		_ = resp.Body.Close()
		reader = io.NopCloser(bytes.NewReader(nil))
	}
	return &BlobReadResult{
		Reader:  reader,
		Digest:  resp.Digest,
		Size:    resp.Size,
		Range:   req.Range,
		Status:  resp.StatusCode,
		Headers: resp.Headers,
		Cache:   CacheBypass,
	}, nil
}

type tagProxy struct {
	client upstream.RegistryClient
}

func (p tagProxy) List(ctx context.Context, req TagRequest) (*TagsResult, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}
	resp, err := p.client.ListTags(ctx, upstream.ListTagsRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		N:             req.N,
		Last:          req.Last,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read tags body: %w", err)
	}
	return &TagsResult{
		Body:    body,
		Headers: resp.Headers,
		Cache:   CacheBypass,
	}, nil
}

type referrerProxy struct {
	client upstream.RegistryClient
}

func (p referrerProxy) Get(ctx context.Context, req ReferrerRequest) (*ReferrersResult, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}
	resp, err := p.client.GetReferrers(ctx, upstream.ReferrersRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Digest:        req.Digest,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read referrers body: %w", err)
	}
	return &ReferrersResult{
		Body:      body,
		MediaType: resp.MediaType,
		Headers:   resp.Headers,
		Cache:     CacheBypass,
	}, nil
}

var (
	_ ManifestService = manifestProxy{}
	_ BlobService     = blobProxy{}
	_ TagService      = tagProxy{}
	_ ReferrerService = referrerProxy{}
)
