package upstreamhttp_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
	"github.com/stretchr/testify/require"
)

func TestDoUsesClientxRequestSettings(t *testing.T) {
	var gotAuth string
	var gotUserAgent string
	var writeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/plain")
		_, writeErr = w.Write([]byte("payload"))
	}))
	defer server.Close()

	cfg := config.UpstreamConfig{
		Auth: config.AuthConfig{Type: "basic", Username: "user", Password: "pass"},
	}
	client, err := upstreamhttp.NewClient(clientfactory.New(slog.Default()), cfg, server.URL, "test.clientx")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, client.Close())
	}()

	headers := http.Header{}
	headers.Set("User-Agent", "regimux-test")
	resp, err := upstreamhttp.Do(context.Background(), client, upstreamhttp.Request{
		Method:  http.MethodGet,
		URL:     server.URL,
		Headers: headers,
		Auth:    cfg.Auth,
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, resp.Body.Close())
	}()

	require.NoError(t, writeErr)
	require.Equal(t, "Basic dXNlcjpwYXNz", gotAuth)
	require.Equal(t, "regimux-test", gotUserAgent)
	require.Equal(t, http.StatusOK, resp.Status)
	require.Equal(t, "text/plain", resp.Headers.Get("Content-Type"))
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "payload", string(body))
}

func TestRawDoAllowsNilHeadersWithBearerAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	resp, err := upstreamhttp.RawDo(context.Background(), server.Client(), upstreamhttp.Request{
		Method: http.MethodHead,
		URL:    server.URL,
		Auth:   config.AuthConfig{Type: "bearer", Token: "token"},
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, resp.Body.Close())
	}()

	require.Equal(t, "Bearer token", gotAuth)
	require.Equal(t, http.StatusNoContent, resp.Status)
}
