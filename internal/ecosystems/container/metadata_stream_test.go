package container_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

type largeMetadataService struct{ body []byte }

func (s largeMetadataService) Get(_ context.Context, _ cache.ManifestRequest) (*cache.CachedManifest, error) {
	return &cache.CachedManifest{
		Body: s.body, Size: int64(len(s.body)), MediaType: distribution.MediaTypeOCIManifest,
	}, nil
}

func TestRegistryMetadataSpoolRemovedWhenClientCancels(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	service := largeMetadataService{body: bytes.Repeat([]byte("x"), 8<<20)}
	baseURL := startAPIServer(t, container.NewRegistryEndpoint(service, nil, nil, nil, nil))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v2/hub/library/test/manifests/latest", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{DialContext: dialSlowMetadataClient}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	waitMetadataSpools(t, tmp, 1)
	cancel()
	waitMetadataSpools(t, tmp, 0)
}

func dialSlowMetadataClient(ctx context.Context, network, address string) (net.Conn, error) {
	var dialer net.Dialer
	connection, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf("dial metadata test server: %w", err)
	}
	tcp, ok := connection.(*net.TCPConn)
	if !ok {
		return nil, errors.Join(errors.New("metadata test requires TCP"), connection.Close())
	}
	// Bound the receive window so the response cannot fit in kernel buffers
	// before the test cancels its deliberately unread body.
	if err := tcp.SetReadBuffer(1024); err != nil {
		return nil, fmt.Errorf("limit metadata test receive buffer: %w", errors.Join(err, connection.Close()))
	}
	return connection, nil
}

func waitMetadataSpools(t *testing.T, directory string, count int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		files, err := filepath.Glob(filepath.Join(directory, "regimux-oci-metadata-*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == count {
			return
		}
		if time.Now().After(deadline) {
			entries, readErr := os.ReadDir(directory)
			t.Fatalf("spools=%d, want %d; entries=%v read error=%v", len(files), count, entries, readErr)
		}
		time.Sleep(time.Millisecond)
	}
}
