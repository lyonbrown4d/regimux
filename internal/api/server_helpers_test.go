package api_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/samber/oops"
)

func startAPIServer(t *testing.T, endpoints ...httpx.Endpoint) string {
	t.Helper()
	all := collectionlist.NewList[httpx.Endpoint]()
	for _, endpoint := range endpoints {
		all.Add(endpoint)
	}
	return startAPIServerWithOptions(t, api.Options{Endpoints: all})
}

func startAPIServerWithOptions(t *testing.T, opts api.Options) string {
	t.Helper()

	addr := freeTCPAddr(t)
	opts.Listen = addr
	endpoints := collectionlist.NewList[httpx.Endpoint](api.NewHealthEndpoint())
	if opts.Endpoints != nil {
		opts.Endpoints.Range(func(_ int, endpoint httpx.Endpoint) bool {
			endpoints.Add(endpoint)
			return true
		})
	}
	opts.Endpoints = endpoints
	server := api.NewServer(opts)
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("start api server: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := server.Stop(ctx); err != nil {
			t.Fatalf("stop api server: %v", err)
		}
	})

	baseURL := "http://" + addr
	waitForHTTP(t, baseURL+"/healthz")
	return baseURL
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate tcp listener: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close tcp listener: %v", err)
	}
	return addr
}

func waitForHTTP(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := httpGetWithClient(client, url)
		if err == nil {
			readHTTPResponse(t, resp)
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server did not become ready at %s", url)
}

func httpGet(t *testing.T, url string) *http.Response {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := httpGetWithClient(client, url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	return resp
}

func httpGetWithClient(client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, oops.Wrapf(err, "build test request")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, oops.Wrapf(err, "send test request")
	}
	return resp, nil
}

func readHTTPResponse(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		t.Fatalf("read response body: %v", readErr)
	}
	if closeErr != nil {
		t.Fatalf("close response body: %v", closeErr)
	}
	return body
}
