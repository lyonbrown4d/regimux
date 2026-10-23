package dist_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/dist"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestDistTransferBodyProgressOutlivesHeaderTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(testZIPBody)))
		if _, err := w.Write([]byte(testZIPBody[:5])); err != nil {
			t.Error(err)
			return
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
		if _, err := w.Write([]byte(testZIPBody[5:])); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	service := newTimedDistService(t, server.URL)
	response, err := service.Get(t.Context(), dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	requireNoError(t, "get archive with slow body", err)
	if got := readResponse(t, response); got != testZIPBody {
		t.Fatalf("body=%q want=%q", got, testZIPBody)
	}
}

func newTimedDistService(t *testing.T, upstreamURL string) *dist.Service {
	t.Helper()
	metadata, err := meta.OpenSQLiteWithOptions(t.Context(), meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open timed-transfer metadata", err)
	t.Cleanup(func() { requireNoError(t, "close timed-transfer metadata", metadata.Close()) })
	objects, err := object.NewLocal(t.TempDir())
	requireNoError(t, "open timed-transfer objects", err)
	return dist.NewService(dist.ServiceDependencies{
		Config: config.Config{Dist: config.DistEcosystemConfig{"gradle": {
			Registry: upstreamURL, HTTP: config.HTTPConfig{AttemptTimeout: 100 * time.Millisecond},
		}}}, Metadata: metadata, Objects: objects,
	})
}
