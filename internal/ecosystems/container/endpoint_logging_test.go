package container_test

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestRegistryEndpointLogsRegistryRequestFields(t *testing.T) {
	manifestDigest := endpointTestDigest("m")
	manifests := endpointManifestService{
		manifest: cachedEndpointManifest(
			manifestDigest,
			distribution.MediaTypeOCIManifest,
			endpointImageManifestBody(t, endpointTestDigest("c")),
		),
	}
	handler := newRegistryLogHandler()
	endpoint := container.NewRegistryEndpoint(&manifests, nil, nil, nil, slog.New(handler))
	baseURL := startAPIServer(t, endpoint)

	resp := httpGet(t, baseURL+"/v2/hub/library/alpine/manifests/latest")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}

	record := handler.findMessage(t, "registry request completed")
	assertRegistryLogAttr(t, record, "route", "registry.manifest")
	assertRegistryLogAttr(t, record, "method", http.MethodGet)
	assertRegistryLogAttr(t, record, "status", int64(http.StatusOK))
	assertRegistryLogAttr(t, record, "cache_status", "miss")
	assertRegistryLogAttr(t, record, "alias", "hub")
	assertRegistryLogAttr(t, record, "repository", "library/alpine")
	assertRegistryLogAttr(t, record, "reference", "latest")
	assertRegistryLogAttr(t, record, "digest", manifestDigest)
	if got := record.attrs["response_size"]; got == nil {
		t.Fatalf("missing response_size attr in %#v", record.attrs)
	}
}

type registryLogHandler struct {
	mu      sync.Mutex
	records []registryLogRecord
}

type registryLogRecord struct {
	level   slog.Level
	message string
	attrs   map[string]any
}

func newRegistryLogHandler() *registryLogHandler {
	return &registryLogHandler{}
}

func (h *registryLogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *registryLogHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := make(map[string]any, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Any()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, registryLogRecord{
		level:   record.Level,
		message: record.Message,
		attrs:   attrs,
	})
	return nil
}

func (h *registryLogHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h *registryLogHandler) WithGroup(string) slog.Handler {
	return h
}

func (h *registryLogHandler) findMessage(t *testing.T, message string) registryLogRecord {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.records {
		if h.records[i].message == message {
			return h.records[i]
		}
	}
	t.Fatalf("missing log message %q in %#v", message, h.records)
	return registryLogRecord{}
}

func assertRegistryLogAttr(t *testing.T, record registryLogRecord, key string, want any) {
	t.Helper()
	if got := record.attrs[key]; got != want {
		t.Fatalf("log attr %s = %#v, want %#v in %#v", key, got, want, record.attrs)
	}
}
