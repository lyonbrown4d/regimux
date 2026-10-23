package pypi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

func TestPackageTransferRecoversRealIdleTimeout(t *testing.T) {
	fixture, _, _ := newPackageTransferFixture(t, false)
	fixture.body = fixture.body[:progressWheelPrefix+progressWheelChunk]
	digest := sha256.Sum256(fixture.body)
	fixture.digest = hex.EncodeToString(digest[:])
	idle := &idlePackageUpstream{fixture: fixture}
	upstream := httptest.NewServer(http.HandlerFunc(idle.serveHTTP))
	t.Cleanup(upstream.Close)
	service := pypi.NewService(pypi.ServiceDependencies{
		Config: config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {
			Registry: upstream.URL, HTTP: config.HTTPConfig{AttemptTimeout: 100 * time.Millisecond},
		}}},
		Metadata: fixture.metadata, Objects: fixture.objects,
	})
	client, proxyURL := startStdProxy(t, service)
	client.Timeout = 20 * time.Second
	tail := packageTailFor(t, upstream.URL, "/packages/demo-1.0-py3-none-any.whl")
	fixture.key = meta.TagKey{Alias: "pypi", Repository: "pypi/packages", Reference: tail[len("packages/"):]}
	query := url.Values{"__regimux_project": {"demo"}, "__regimux_sha256": {fixture.digest}}
	requestURL := proxyURL + "/pypi/pypi/" + tail + "?" + query.Encode()
	started := time.Now()
	verifyPackageTransfer(t, client, requestURL, fixture, "miss")
	if elapsed := time.Since(started); elapsed < upstreamhttp.DefaultBodyIdleTimeout || elapsed >= client.Timeout {
		t.Errorf("idle recovery took %s, want at least %s and below client deadline %s", elapsed, upstreamhttp.DefaultBodyIdleTimeout, client.Timeout)
	}
	if !idle.attemptCanceled.Load() || fixture.ranges.Load() != 1 || fixture.calls.Load() != 2 {
		t.Errorf("idle recovery attemptCanceled=%t ranges=%d calls=%d", idle.attemptCanceled.Load(), fixture.ranges.Load(), fixture.calls.Load())
	}
	waitPackageTransferCache(t, fixture)
	verifyPackageTransfer(t, client, requestURL, fixture, "hit")
}

type idlePackageUpstream struct {
	fixture         *packageTransferFixture
	attemptCanceled atomic.Bool
}

func (s *idlePackageUpstream) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f := s.fixture
	f.calls.Add(1)
	w.Header().Set("ETag", progressWheelETag)
	w.Header().Set("Content-Type", "application/octet-stream")
	if r.Header.Get("Range") != "" {
		f.serveResume(w, r)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(f.body)))
	if _, err := w.Write(f.body[:progressWheelPrefix]); err != nil {
		f.t.Errorf("write wheel prefix before idle: %v", err)
		return
	}
	if err := http.NewResponseController(w).Flush(); err != nil {
		f.t.Errorf("flush wheel prefix before idle: %v", err)
		return
	}
	// Keep the socket open beyond the production idle budget. The attempt must
	// be canceled and replaced by a verified range, while the caller stays live.
	timer := time.NewTimer(31 * time.Second)
	defer timer.Stop()
	select {
	case <-r.Context().Done():
		s.attemptCanceled.Store(true)
	case <-timer.C:
		if _, err := w.Write(f.body[progressWheelPrefix:]); err != nil {
			f.t.Errorf("write wheel tail after idle: %v", err)
		}
	}
}
