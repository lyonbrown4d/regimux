package upstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
