package upstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestClientGetBlobLatencySchedulerSpreadsConcurrentLayers(t *testing.T) {
	t.Parallel()

	const (
		firstDigest  = "sha256:1111111111111111"
		secondDigest = "sha256:2222222222222222"
	)
	var fastBlobRequests atomic.Int32
	fast := httptest.NewServer(scheduledBlobHandler(t, "fast", 0, http.StatusOK, &fastBlobRequests))
	defer fast.Close()

	var secondBlobRequests atomic.Int32
	second := httptest.NewServer(scheduledBlobHandler(t, "second", 40*time.Millisecond, http.StatusOK, &secondBlobRequests))
	defer second.Close()

	client := newLatencyBlobSchedulerClient(fast.URL, second.URL)
	requireNoError(t, client.ProbeAlias(context.Background(), "hub"), "ProbeAlias")

	firstResp, err := client.GetBlob(context.Background(), upstream.GetBlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Digest:        firstDigest,
	})
	requireNoError(t, err, "first GetBlob")

	secondResp, err := client.GetBlob(context.Background(), upstream.GetBlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Digest:        secondDigest,
	})
	requireNoError(t, err, "second GetBlob")

	requireEqual(t, readAndClose(t, secondResp.Body), "second", "second blob body")
	requireEqual(t, readAndClose(t, firstResp.Body), "fast", "first blob body")
	requireEqual(t, fastBlobRequests.Load(), int32(1), "fast blob requests")
	requireEqual(t, secondBlobRequests.Load(), int32(1), "second blob requests")
}

func TestClientGetBlobLatencySchedulerSkipsCooldownEndpointAfterFailure(t *testing.T) {
	t.Parallel()

	const (
		firstDigest  = "sha256:aaaaaaaaaaaaaaaa"
		secondDigest = "sha256:bbbbbbbbbbbbbbbb"
	)
	var failingBlobRequests atomic.Int32
	failing := httptest.NewServer(scheduledBlobHandler(t, "failing", 0, http.StatusBadGateway, &failingBlobRequests))
	defer failing.Close()

	var healthyBlobRequests atomic.Int32
	healthy := httptest.NewServer(scheduledBlobHandler(t, "healthy", 40*time.Millisecond, http.StatusOK, &healthyBlobRequests))
	defer healthy.Close()

	client := newLatencyBlobSchedulerClient(failing.URL, healthy.URL)
	requireNoError(t, client.ProbeAlias(context.Background(), "hub"), "ProbeAlias")

	firstResp, err := client.GetBlob(context.Background(), upstream.GetBlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Digest:        firstDigest,
	})
	requireNoError(t, err, "first GetBlob")
	requireEqual(t, readAndClose(t, firstResp.Body), "healthy", "first blob body")

	secondResp, err := client.GetBlob(context.Background(), upstream.GetBlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Digest:        secondDigest,
	})
	requireNoError(t, err, "second GetBlob")
	requireEqual(t, readAndClose(t, secondResp.Body), "healthy", "second blob body")
	requireEqual(t, failingBlobRequests.Load(), int32(1), "failing blob requests")
	requireEqual(t, healthyBlobRequests.Load(), int32(2), "healthy blob requests")
}

func TestClientGetBlobDowngradesContentInconsistentMirror(t *testing.T) {
	t.Parallel()

	const digest = "sha256:cccccccccccccccc"
	var inconsistentRequests atomic.Int32
	inconsistent := httptest.NewServer(blobDigestHeaderHandler(t, "bad", "sha256:dddddddddddddddd", &inconsistentRequests))
	defer inconsistent.Close()

	var healthyRequests atomic.Int32
	healthy := httptest.NewServer(blobDigestHeaderHandler(t, "healthy", digest, &healthyRequests))
	defer healthy.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors:      []string{inconsistent.URL, healthy.URL},
			MirrorPolicy: "ordered",
			Blob: upstream.BlobConfig{
				MirrorPolicy: "ordered",
			},
			Probe: upstream.ProbeConfig{
				Cooldown: time.Minute,
			},
		},
	})

	resp, err := client.GetBlob(context.Background(), upstream.GetBlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Digest:        digest,
	})
	requireNoError(t, err, "GetBlob")
	requireEqual(t, readAndClose(t, resp.Body), "healthy", "blob body")
	requireEqual(t, inconsistentRequests.Load(), int32(1), "inconsistent blob requests")
	requireEqual(t, healthyRequests.Load(), int32(1), "healthy blob requests")

	snapshot := client.Snapshot(time.Now())
	got := endpointSnapshot(t, snapshot, inconsistent.URL)
	if !got.Health.InDegraded || got.Health.ContentMismatchCount != 1 {
		t.Fatalf("inconsistent endpoint was not degraded: %#v", got.Health)
	}
}

func TestClientLoadEndpointHealthAvoidsLatencyColdStart(t *testing.T) {
	t.Parallel()

	const digest = "sha256:eeeeeeeeeeeeeeee"
	var fastBlobRequests atomic.Int32
	fast := httptest.NewServer(scheduledBlobHandler(t, "fast", 0, http.StatusOK, &fastBlobRequests))
	defer fast.Close()

	var slowBlobRequests atomic.Int32
	slow := httptest.NewServer(scheduledBlobHandler(t, "slow", 80*time.Millisecond, http.StatusOK, &slowBlobRequests))
	defer slow.Close()

	ctx := context.Background()
	store, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, err, "open metadata store")
	t.Cleanup(func() {
		requireNoError(t, store.Close(), "close metadata store")
	})

	configs := map[string]upstream.Config{
		"hub": {
			Mirrors:      []string{slow.URL, fast.URL},
			MirrorPolicy: "ordered",
			Blob: upstream.BlobConfig{
				MirrorPolicy: "latency",
				TopN:         2,
			},
			Probe: upstream.ProbeConfig{
				Enabled:  true,
				Timeout:  time.Second,
				Cooldown: time.Minute,
			},
		},
	}
	warmingClient := newTestClientWithMetadata(configs, store)
	requireNoError(t, warmingClient.ProbeAlias(ctx, "hub"), "ProbeAlias")

	restartedClient := newTestClientWithMetadata(configs, store)
	requireNoError(t, restartedClient.LoadEndpointHealth(ctx), "LoadEndpointHealth")
	resp, err := restartedClient.GetBlob(ctx, upstream.GetBlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Digest:        digest,
	})
	requireNoError(t, err, "GetBlob")
	requireEqual(t, readAndClose(t, resp.Body), "fast", "blob body")
	requireEqual(t, fastBlobRequests.Load(), int32(1), "fast blob requests")
	requireEqual(t, slowBlobRequests.Load(), int32(0), "slow blob requests")
}

func newLatencyBlobSchedulerClient(first, second string) *upstream.Client {
	return newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors:      []string{second, first},
			MirrorPolicy: "ordered",
			Blob: upstream.BlobConfig{
				MirrorPolicy: "latency",
				TopN:         2,
			},
			Probe: upstream.ProbeConfig{
				Enabled:  true,
				Timeout:  time.Second,
				Cooldown: time.Minute,
			},
		},
	})
}

func blobDigestHeaderHandler(t *testing.T, label, headerDigest string, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		requests.Add(1)
		w.Header().Set(distribution.HeaderDockerContentDigest, headerDigest)
		w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(label)))
		writeString(t, w, label)
	}
}

func endpointSnapshot(t *testing.T, snapshot upstream.ClientSnapshot, registry string) upstream.EndpointSnapshot {
	t.Helper()
	var found *upstream.EndpointSnapshot
	snapshot.Upstreams.Range(func(_ int, upstreamSnapshot upstream.UpstreamSnapshot) bool {
		upstreamSnapshot.Endpoints.Range(func(_ int, endpoint upstream.EndpointSnapshot) bool {
			if endpoint.Registry == registry {
				found = &endpoint
				return false
			}
			return true
		})
		return found == nil
	})
	if found != nil {
		return *found
	}
	t.Fatalf("endpoint snapshot not found for %s: %#v", registry, snapshot)
	return upstream.EndpointSnapshot{}
}

func scheduledBlobHandler(t *testing.T, label string, probeDelay time.Duration, blobStatus int, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		if r.URL.Path == "/v2/" {
			if probeDelay > 0 {
				time.Sleep(probeDelay)
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		requests.Add(1)
		digest := strings.TrimPrefix(r.URL.Path, "/v2/library/nginx/blobs/")
		if digest == r.URL.Path {
			t.Fatalf("unexpected blob path: %s", r.URL.Path)
		}
		if blobStatus < http.StatusOK || blobStatus >= http.StatusMultipleChoices {
			w.WriteHeader(blobStatus)
			return
		}
		body := label
		w.Header().Set(distribution.HeaderDockerContentDigest, digest)
		w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
		writeString(t, w, body)
	}
}
