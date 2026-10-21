package api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"testing"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/config"
)

type retainedRouteInput struct {
	Alias string         `path:"alias"`
	Tail  httpx.PathTail `path:"tail"`
}

type retainedRouteOutput struct {
	Status int
	Body   httpx.ResponseStream
}

type retainedRouteProbe struct {
	firstAlias chan string
	firstOnce  *sync.Once
}

func (route retainedRouteProbe) EndpointSpec() httpx.EndpointSpec {
	return httpx.EndpointSpec{
		Tags:       httpx.Tags("request lifetime"),
		Security:   httpx.SecurityRequirements(),
		Parameters: httpx.Parameters(),
		Extensions: httpx.Extensions(nil),
	}
}

func (route retainedRouteProbe) Register(registrar httpx.Registrar) {
	group := registrar.Scope()
	httpx.MustGroupGet(group, "maven/{alias}/{tail...}", route.get)
	httpx.MustGroupGet(group, "dist/{alias}/{tail...}", route.get)
}

func (route retainedRouteProbe) get(_ context.Context, input *retainedRouteInput) (*retainedRouteOutput, error) {
	route.firstOnce.Do(func() { route.firstAlias <- input.Alias })
	return &retainedRouteOutput{Status: http.StatusOK, Body: httpx.StreamBytes([]byte("ok"))}, nil
}

func TestServerRetainsPathBoundParametersAfterKeepAliveReuse(t *testing.T) {
	probe := retainedRouteProbe{firstAlias: make(chan string, 1), firstOnce: &sync.Once{}}
	cfg := config.DefaultConfig()
	baseURL := startAPIServerWithOptions(t, api.Options{
		Middleware: cfg.Server.Middleware,
		Endpoints:  collectionlist.NewList[httpx.Endpoint](probe),
	})

	transport := &http.Transport{MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	t.Cleanup(func() { transport.CloseIdleConnections() })

	first := requestRetainedRoute(t, client, baseURL+"/maven/central/com/acme/demo.jar")
	if first.reused {
		t.Fatal("first request unexpectedly reused a connection")
	}
	firstAlias := receiveRetainedAlias(t, probe.firstAlias)
	for i := range 128 {
		result := requestRetainedRoute(t, client, baseURL+"/dist/nodejs/v1/demo.tar.xz")
		if !result.reused {
			t.Fatalf("request %d did not reuse the keep-alive connection", i+2)
		}
	}
	if firstAlias != "central" {
		t.Fatalf("first path-bound alias changed after request-context reuse: %q, want central", firstAlias)
	}
}

type retainedRouteResponse struct {
	reused bool
}

func requestRetainedRoute(t *testing.T, client *http.Client, url string) retainedRouteResponse {
	t.Helper()
	var reused bool
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(t.Context(), trace), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read/close response body: read=%v close=%v", readErr, closeErr)
	}
	if response.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("response = %d %q, want 200 ok", response.StatusCode, body)
	}
	return retainedRouteResponse{reused: reused}
}

func receiveRetainedAlias(t *testing.T, aliases <-chan string) string {
	t.Helper()
	select {
	case alias := <-aliases:
		return alias
	case <-time.After(time.Second):
		t.Fatal("route handler did not report parameters")
		return ""
	case <-t.Context().Done():
		t.Fatal("test context ended before route handler reported parameters")
		return ""
	}
}
