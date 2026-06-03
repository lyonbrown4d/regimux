package api_test

import (
	"net/http"
	"testing"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/config"
)

type adminTestRoute struct{}

func (adminTestRoute) RegisterFiber(app *fiber.App) {
	app.Get("/admin", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
}

func TestSecurityHeadersAllowAdminCDNByDefault(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	baseURL := startAPIServerWithOptions(t, api.Options{
		Middleware:  cfg.Server.Middleware,
		FiberRoutes: collectionlist.NewList[api.FiberRoute](adminTestRoute{}),
	})
	resp := httpGet(t, baseURL+"/admin")
	defer readHTTPResponse(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Cross-Origin-Embedder-Policy"); got != "unsafe-none" {
		t.Fatalf("Cross-Origin-Embedder-Policy = %q, want unsafe-none", got)
	}
}
