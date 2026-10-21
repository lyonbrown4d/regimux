package npm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/spool"
)

func TestAcquireMetadataSlotHonorsCancellationAndRelease(t *testing.T) {
	slots := make(chan struct{}, 1)
	release, err := acquireMetadataSlot(context.Background(), slots)
	if err != nil {
		t.Fatalf("acquire first slot: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireMetadataSlot(ctx, slots); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire canceled slot error = %v, want context.Canceled", err)
	}

	release()
	release()
	if _, err := acquireMetadataSlot(context.Background(), slots); err != nil {
		t.Fatalf("acquire released slot: %v", err)
	}
}

func TestPrepareMetadataSpoolsRewrittenBody(t *testing.T) {
	service := &Service{logger: slog.Default()}
	body := `{"versions":{"1.0.0":{"dist":{"tarball":"https://registry.npmjs.org/demo/-/demo-1.0.0.tgz"}}}}`
	prepared, err := service.prepareFetched(
		Request{ProxyBaseURL: "https://cache.example.test"},
		route{Alias: "npmjs", Kind: routeMetadata},
		&upstreamFetch{
			status:          200,
			headers:         http.Header{"Content-Type": {"application/json"}},
			body:            io.NopCloser(strings.NewReader(body)),
			requestURL:      "https://registry.npmjs.org/demo",
			registryBaseURL: "https://registry.npmjs.org",
		},
	)
	if err != nil {
		t.Fatalf("prepare metadata: %v", err)
	}
	tmp, ok := prepared.body.(*spool.TempBody)
	if !ok {
		t.Fatalf("prepared body type = %T, want spooled temp body", prepared.body)
	}
	defer func() {
		if closeErr := tmp.Close(); closeErr != nil {
			t.Errorf("close spooled metadata: %v", closeErr)
		}
	}()
	got, err := io.ReadAll(tmp)
	if err != nil {
		t.Fatalf("read spooled metadata: %v", err)
	}
	if !bytes.Contains(got, []byte("https://cache.example.test/npm/npmjs/demo/-/demo-1.0.0.tgz")) {
		t.Fatalf("spooled metadata did not contain rewritten tarball: %s", got)
	}
}

func TestPrepareMetadataAcceptsNilHeaders(t *testing.T) {
	service := &Service{logger: slog.Default()}
	body := `{"versions":{}}`
	prepared, err := service.prepareFetched(
		Request{},
		route{Alias: "npmjs", Kind: routeMetadata},
		&upstreamFetch{
			status:     http.StatusOK,
			body:       io.NopCloser(strings.NewReader(body)),
			requestURL: "https://registry.npmjs.org/demo",
		},
	)
	if err != nil {
		t.Fatalf("prepare metadata with nil headers: %v", err)
	}
	tmp, ok := prepared.body.(*spool.TempBody)
	if !ok {
		t.Fatalf("prepared body type = %T, want spooled temp body", prepared.body)
	}
	defer func() {
		if err := tmp.Close(); err != nil {
			t.Errorf("close spooled metadata: %v", err)
		}
	}()
	if prepared.headers.Get("X-Regimux-Metadata-Format") != npmMetadataFormat {
		t.Fatalf("metadata format header = %q", prepared.headers.Get("X-Regimux-Metadata-Format"))
	}
}
