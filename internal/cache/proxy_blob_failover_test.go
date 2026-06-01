package cache_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestBlobProxyRetriesMirrorAfterDigestMismatch(t *testing.T) {
	ctx := context.Background()
	goodBody := []byte("good")
	digest := testDigestFor(goodBody)

	var inconsistentRequests atomic.Int32
	inconsistent := httptest.NewServer(cacheBlobHandler(t, digest, []byte("bad"), &inconsistentRequests))
	defer inconsistent.Close()

	var healthyRequests atomic.Int32
	healthy := httptest.NewServer(cacheBlobHandler(t, digest, goodBody, &healthyRequests))
	defer healthy.Close()

	client := newCacheTestUpstreamClient(map[string]upstream.Config{
		"hub": {
			Mirrors:      []string{inconsistent.URL, healthy.URL},
			MirrorPolicy: "ordered",
			Blob: upstream.BlobConfig{
				MirrorPolicy: "ordered",
			},
		},
	})
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(client, metadata, objects, nil, config.Config{})

	result, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("blob get: %v", err)
	}

	body := readAndClose(t, result.Reader)
	if result.Cache != cache.CacheMiss || !bytes.Equal(body, goodBody) {
		t.Fatalf("unexpected blob result: cache=%s body=%q", result.Cache, body)
	}
	assertObjectPresence(ctx, t, objects, digest, true)
	requireCounter(t, inconsistentRequests.Load(), 1, "inconsistent mirror requests")
	requireCounter(t, healthyRequests.Load(), 1, "healthy mirror requests")
}

func newCacheTestUpstreamClient(configs map[string]upstream.Config) *upstream.Client {
	ordered := collectionmapping.NewOrderedMapWithCapacity[string, upstream.Config](len(configs))
	aliases := collectionlist.NewList(collectionmapping.NewMapFrom(configs).Keys()...).
		Sort(strings.Compare).
		Values()
	for _, alias := range aliases {
		ordered.Set(alias, configs[alias])
	}
	return upstream.NewClient(upstream.ClientDependencies{Configs: ordered})
}

func cacheBlobHandler(t *testing.T, digest string, body []byte, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		requests.Add(1)
		if r.URL.Path != "/v2/library/alpine/blobs/"+digest {
			t.Fatalf("blob path = %s, want /v2/library/alpine/blobs/%s", r.URL.Path, digest)
		}
		w.Header().Set(distribution.HeaderDockerContentDigest, digest)
		w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
		if _, err := w.Write(body); err != nil {
			t.Fatalf("write blob body: %v", err)
		}
	}
}

func requireCounter(t *testing.T, got, want int32, label string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %d, want %d", label, got, want)
	}
}
