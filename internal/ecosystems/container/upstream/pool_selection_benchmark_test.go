package upstream_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func BenchmarkClientGetManifestOrderedMirrors(b *testing.B) {
	silenceBenchmarkLogs(b)
	servers := benchmarkRegistryServers(b, benchmarkManifestHandler)
	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors:      benchmarkServerURLs(servers),
			MirrorPolicy: "ordered",
		},
	})

	b.ResetTimer()
	for b.Loop() {
		resp, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
			UpstreamAlias: "hub",
			Repo:          "library/nginx",
			Reference:     "latest",
		})
		if err != nil {
			b.Fatalf("get manifest: %v", err)
		}
		closeBenchmarkBody(b, resp.Body)
	}
}

func BenchmarkClientGetBlobOrderedMirrors(b *testing.B) {
	silenceBenchmarkLogs(b)
	servers := benchmarkRegistryServers(b, benchmarkBlobHandler)
	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors:      benchmarkServerURLs(servers),
			MirrorPolicy: "ordered",
			Blob: upstream.BlobConfig{
				MirrorPolicy: "ordered",
			},
		},
	})

	b.ResetTimer()
	for b.Loop() {
		resp, err := client.GetBlob(context.Background(), upstream.GetBlobRequest{
			UpstreamAlias: "hub",
			Repo:          "library/nginx",
			Digest:        "sha256:abcdef0123456789",
		})
		if err != nil {
			b.Fatalf("get blob: %v", err)
		}
		closeBenchmarkBody(b, resp.Body)
	}
}

func benchmarkRegistryServers(b *testing.B, handler http.HandlerFunc) []*httptest.Server {
	b.Helper()
	servers := make([]*httptest.Server, 0, 16)
	for range 16 {
		server := httptest.NewServer(handler)
		servers = append(servers, server)
		b.Cleanup(server.Close)
	}
	return servers
}

func benchmarkServerURLs(servers []*httptest.Server) []string {
	urls := make([]string, 0, len(servers))
	for _, server := range servers {
		urls = append(urls, server.URL)
	}
	return urls
}

func benchmarkManifestHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v2/library/nginx/manifests/latest" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body := `{"schemaVersion":2}`
	w.Header().Set(distribution.HeaderDockerContentDigest, "sha256:abc")
	w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
	w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
	writeBenchmarkResponse(w, body)
}

func benchmarkBlobHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v2/library/nginx/blobs/sha256:abcdef0123456789" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body := "benchmark blob"
	w.Header().Set(distribution.HeaderDockerContentDigest, "sha256:abcdef0123456789")
	w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
	writeBenchmarkResponse(w, body)
}

func writeBenchmarkResponse(w http.ResponseWriter, body string) {
	if _, err := io.WriteString(w, body); err != nil {
		panic(err)
	}
}
func closeBenchmarkBody(b *testing.B, body io.ReadCloser) {
	b.Helper()
	if _, err := io.Copy(io.Discard, body); err != nil {
		b.Fatalf("drain response body: %v", err)
	}
	if err := body.Close(); err != nil {
		b.Fatalf("close response body: %v", err)
	}
}

func silenceBenchmarkLogs(b *testing.B) {
	b.Helper()
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() {
		slog.SetDefault(previous)
	})
}
