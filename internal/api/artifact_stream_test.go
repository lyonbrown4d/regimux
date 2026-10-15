package api_test

import (
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/config"
)

type artifactStreamRoute struct {
	path    string
	release <-chan struct{}
}

func (route artifactStreamRoute) RegisterFiber(app *fiber.App) {
	app.Get(route.path, func(c fiber.Ctx) error {
		reader, writer := io.Pipe()
		go func() {
			defer writer.Close()
			if _, err := io.WriteString(writer, "wheel-"); err != nil {
				return
			}
			<-route.release
			_, _ = io.WriteString(writer, "body")
		}()
		c.Set(fiber.HeaderContentType, "application/octet-stream")
		c.Set(fiber.HeaderETag, `"upstream-artifact"`)
		c.Response().SetBodyStream(reader, len("wheel-body"))
		c.Response().ImmediateHeaderFlush = true
		return nil
	})
}

func TestDefaultMiddlewareStreamsArtifactHeadersBeforeBodyCompletes(t *testing.T) {
	for _, path := range []string{
		"/pypi/default/packages/demo.whl",
		"/maven/central/demo/1/demo-1.jar",
		"/go/default/example.org/demo/@v/v1.0.0.zip",
		"/npm/default/demo/-/demo-1.0.0.tgz",
		"/dist/nodejs/v1/demo.tar.xz",
		"/v2/hub/demo/blobs/sha256:abc",
	} {
		t.Run(path, func(t *testing.T) {
			release := make(chan struct{})
			var once sync.Once
			releaseBody := func() { once.Do(func() { close(release) }) }
			defer releaseBody()
			cfg := config.DefaultConfig()
			base := startAPIServerWithOptions(t, api.Options{
				Middleware: cfg.Server.Middleware,
				FiberRoutes: collectionlist.NewList[api.FiberRoute](artifactStreamRoute{
					path: path, release: release,
				}),
			})
			client := &http.Client{Timeout: 2 * time.Second}
			defer client.CloseIdleConnections()
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+path, http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Accept-Encoding", "gzip")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("headers must arrive before releasing the complete body: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK || resp.Header.Get("ETag") != `"upstream-artifact"` {
				t.Fatalf("artifact status/ETag changed: %d %v", resp.StatusCode, resp.Header)
			}
			if resp.ContentLength != int64(len("wheel-body")) || resp.Header.Get("Content-Encoding") != "" {
				t.Fatalf("artifact representation was transformed: %v", resp.Header)
			}
			releaseBody()
			if body := readHTTPResponse(t, resp); string(body) != "wheel-body" {
				t.Fatalf("body = %q", body)
			}
		})
	}
}

func TestDefaultMiddlewareStillGeneratesAdminETag(t *testing.T) {
	cfg := config.DefaultConfig()
	base := startAPIServerWithOptions(t, api.Options{
		Middleware:  cfg.Server.Middleware,
		FiberRoutes: collectionlist.NewList[api.FiberRoute](adminTestRoute{}),
	})
	resp := httpGet(t, base+"/admin")
	defer readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("ETag") == "" {
		t.Fatalf("admin ETag missing: %d %v", resp.StatusCode, resp.Header)
	}
}
