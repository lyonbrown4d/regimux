package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/arcgolabs/httpx"
	"github.com/danielgtaylor/huma/v2"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestRegistryEndpointAppliesDefaultNamespace(t *testing.T) {
	t.Parallel()

	manifests := &recordingManifestService{}
	endpoint := NewRegistryEndpointFromConfig(
		manifests,
		nil,
		nil,
		nil,
		nil,
		config.Config{
			Upstreams: map[string]config.UpstreamConfig{
				"hub": {DefaultNamespace: "library"},
			},
		},
	)

	out, err := endpoint.dispatch(context.Background(), &registryInput{
		Alias: "hub",
		Tail:  httpx.PathTail{Value: "hello-world/manifests/latest"},
	}, http.MethodGet)
	if err != nil {
		t.Fatalf("dispatch returned error: %v", err)
	}
	if out.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", out.Status)
	}
	if manifests.repo != "library/hello-world" {
		t.Fatalf("repo = %q, want library/hello-world", manifests.repo)
	}
}

func TestRegistryEndpointInvalidDigestReturnsDigestInvalid(t *testing.T) {
	t.Parallel()

	endpoint := NewRegistryEndpoint(nil, nil, nil, nil, nil)
	out, err := endpoint.dispatch(context.Background(), &registryInput{
		Alias: "hub",
		Tail:  httpx.PathTail{Value: "library/alpine/blobs/not-a-digest"},
	}, http.MethodGet)
	if err != nil {
		t.Fatalf("dispatch returned error: %v", err)
	}
	if out.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", out.Status)
	}

	var body distribution.ErrorResponse
	bodyBytes := streamResponse(t, out.Body)
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("unmarshal error body: %v body=%q", err, bodyBytes)
	}
	if len(body.Errors) != 1 || body.Errors[0].Code != distribution.CodeDigestInvalid {
		t.Fatalf("error body = %#v, want DIGEST_INVALID", body.Errors)
	}
}

type recordingManifestService struct {
	repo string
}

func (s *recordingManifestService) Get(_ context.Context, req cache.ManifestRequest) (*cache.CachedManifest, error) {
	s.repo = req.Repo
	return &cache.CachedManifest{
		Digest:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		MediaType: "application/vnd.oci.image.manifest.v1+json",
		Size:      2,
		Body:      []byte("{}"),
		Headers:   http.Header{"Content-Length": {"2"}},
		Cache:     cache.CacheBypass,
	}, nil
}

var _ cache.ManifestService = (*recordingManifestService)(nil)

func streamResponse(t *testing.T, stream httpx.ResponseStream) []byte {
	t.Helper()
	out := &responseCapture{}
	stream(out)
	return out.body
}

type responseCapture struct {
	body   []byte
	status int
	header http.Header
}

func (c *responseCapture) Operation() *huma.Operation {
	return nil
}

func (c *responseCapture) Context() context.Context {
	return context.Background()
}

func (c *responseCapture) TLS() *tls.ConnectionState {
	return nil
}

func (c *responseCapture) Version() huma.ProtoVersion {
	return huma.ProtoVersion{Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1}
}

func (c *responseCapture) Method() string {
	return http.MethodGet
}

func (c *responseCapture) Host() string {
	return "example.test"
}

func (c *responseCapture) RemoteAddr() string {
	return "127.0.0.1:12345"
}

func (c *responseCapture) URL() url.URL {
	return url.URL{Path: "/"}
}

func (c *responseCapture) Param(string) string {
	return ""
}

func (c *responseCapture) Query(string) string {
	return ""
}

func (c *responseCapture) Header(string) string {
	return ""
}

func (c *responseCapture) EachHeader(func(name, value string)) {}

func (c *responseCapture) BodyReader() io.Reader {
	return http.NoBody
}

func (c *responseCapture) GetMultipartForm() (*multipart.Form, error) {
	return nil, nil
}

func (c *responseCapture) SetReadDeadline(time.Time) error {
	return nil
}

func (c *responseCapture) SetStatus(code int) {
	c.status = code
}

func (c *responseCapture) Status() int {
	return c.status
}

func (c *responseCapture) SetHeader(name, value string) {
	if c.header == nil {
		c.header = http.Header{}
	}
	c.header.Set(name, value)
}

func (c *responseCapture) AppendHeader(name, value string) {
	if c.header == nil {
		c.header = http.Header{}
	}
	c.header.Add(name, value)
}

func (c *responseCapture) BodyWriter() io.Writer {
	return responseBodyWriter{capture: c}
}

type responseBodyWriter struct {
	capture *responseCapture
}

func (w responseBodyWriter) Write(data []byte) (int, error) {
	w.capture.body = append(w.capture.body, data...)
	return len(data), nil
}
