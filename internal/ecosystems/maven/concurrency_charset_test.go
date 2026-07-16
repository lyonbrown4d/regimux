package maven_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
	"github.com/lyonbrown4d/regimux/internal/testkit"
)

func TestServiceCoalescesConcurrentLegacyCharsetPOMMiss(t *testing.T) {
	const clients = 128
	body := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><project>" +
		"<modelVersion>4.0.0</modelVersion><groupId>org.apache.commons</groupId>" +
		"<artifactId>commons-parent</artifactId><version>28</version><name>Caf\xe9</name></project>"

	var requests atomic.Int64
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
		}
		<-release
		w.Header().Set("Content-Type", "application/xml")
		writeResponse(t, w, body)
	}))
	t.Cleanup(upstream.Close)

	service, _ := newTestService(context.Background(), t, map[string]config.DependencyUpstreamConfig{
		"central": {Registry: upstream.URL},
	})
	run := testkit.StartConcurrent(clients, func() (*maven.Response, error) {
		return service.Get(context.Background(), maven.Request{
			Alias: "central",
			Tail:  "org/apache/commons/commons-parent/28/commons-parent-28.pom",
		})
	})
	testkit.WaitForSignal(t, started)
	releaseOnce.Do(func() { close(release) })

	responses := run.Wait(t)
	for _, response := range responses {
		if response.Status != http.StatusOK {
			closeBody(t, response.Body)
			t.Fatalf("status = %d, want %d", response.Status, http.StatusOK)
		}
		assertBody(t, response, body)
	}
	testkit.RequireOneMiss(t, responses, cacheMiss, cacheHit, func(response *maven.Response) string {
		return response.Cache
	})
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}
