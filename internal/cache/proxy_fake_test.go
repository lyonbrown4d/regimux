package cache_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

type fakeRegistryClient struct {
	blobBody   []byte
	blobDigest string
	blobGets   int
	blobHeads  int

	tagsBody   []byte
	tagsHeader http.Header
	tagsLists  int

	referrersBody []byte
	referrersErr  error
	referrersGets int

	manifestBody      []byte
	manifestMedia     string
	manifestReference string
	manifestGets      int
	manifestHeads     int
	manifestErr       error
}

func (c *fakeRegistryClient) Ping(context.Context, string) error {
	return nil
}

func (c *fakeRegistryClient) GetManifest(_ context.Context, req upstream.GetManifestRequest) (*upstream.ManifestResponse, error) {
	c.manifestGets++
	c.manifestReference = req.Reference
	if req.Method == http.MethodHead {
		c.manifestHeads++
	}
	if c.manifestErr != nil {
		return nil, c.manifestErr
	}

	body := c.manifestBody
	if req.Method == http.MethodHead {
		body = nil
	}
	return &upstream.ManifestResponse{
		Body:      io.NopCloser(bytes.NewReader(body)),
		Digest:    testDigestFor(c.manifestBody),
		MediaType: c.manifestMedia,
		Size:      int64(len(c.manifestBody)),
		Headers:   http.Header{"Content-Type": {c.manifestMedia}},
	}, nil
}

func (c *fakeRegistryClient) GetBlob(_ context.Context, req upstream.GetBlobRequest) (*upstream.BlobResponse, error) {
	body := c.blobBody
	headers := http.Header{
		"Content-Type": {distribution.MediaTypeOctetStream},
	}
	contentLength := len(c.blobBody)

	switch req.Method {
	case http.MethodHead:
		c.blobHeads++
		body = nil
		contentLength = 0
	default:
		c.blobGets++
		if req.Range != nil {
			resolved, resolveErr := req.Range.Resolve(int64(len(c.blobBody)))
			if resolveErr != nil {
				return nil, resolveErr
			}
			body = body[resolved.Start : resolved.End+1]
			contentLength = len(body)
			headers.Set("Content-Range", resolved.ContentRange(int64(len(c.blobBody))))
			headers.Set("Content-Length", strconv.Itoa(contentLength))
			return &upstream.BlobResponse{
				Body:       io.NopCloser(bytes.NewReader(body)),
				Digest:     c.blobDigest,
				Size:       int64(contentLength),
				StatusCode: http.StatusPartialContent,
				Headers:    headers,
			}, nil
		}
	}

	headers.Set("Content-Length", strconv.Itoa(contentLength))
	return &upstream.BlobResponse{
		Body:       io.NopCloser(bytes.NewReader(body)),
		Digest:     c.blobDigest,
		Size:       int64(contentLength),
		StatusCode: http.StatusOK,
		Headers:    headers,
	}, nil
}

func (c *fakeRegistryClient) ListTags(context.Context, upstream.ListTagsRequest) (*upstream.TagsResponse, error) {
	c.tagsLists++
	return &upstream.TagsResponse{
		Body:    io.NopCloser(bytes.NewReader(c.tagsBody)),
		Headers: c.tagsHeader.Clone(),
	}, nil
}

func (c *fakeRegistryClient) GetReferrers(context.Context, upstream.ReferrersRequest) (*upstream.ReferrersResponse, error) {
	c.referrersGets++
	if c.referrersErr != nil {
		return nil, c.referrersErr
	}
	return &upstream.ReferrersResponse{
		Body:      io.NopCloser(bytes.NewReader(c.referrersBody)),
		MediaType: distribution.MediaTypeOCIIndex,
		Headers:   http.Header{"Content-Type": {distribution.MediaTypeOCIIndex}},
	}, nil
}

var _ upstream.RegistryClient = (*fakeRegistryClient)(nil)
