package upstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/upstream"
)

func TestClientProbeAliasDoesNotCancelSiblingEndpointsAfterFailure(t *testing.T) {
	t.Parallel()

	slowStarted := make(chan struct{})
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		<-slowStarted
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer failing.Close()

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		close(slowStarted)
		time.Sleep(25 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer slow.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors: []string{failing.URL, slow.URL},
			Probe: upstream.ProbeConfig{
				Enabled: true,
				Timeout: time.Second,
			},
		},
	})

	requireNoError(t, client.ProbeAlias(context.Background(), "hub"), "ProbeAlias")
}
