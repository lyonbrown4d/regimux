package admin_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/admin"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestServiceRendersDashboardAndPartials(t *testing.T) {
	ctx := context.Background()
	cfg := config.DefaultConfig()
	cfg.Auth.Users = map[string]config.AuthUserConfig{
		"alice": {
			PasswordHash: "sha256:test",
			Repositories: []string{"hub/library/*"},
			Groups:       []string{"operators"},
		},
	}
	metadata := newMetadataStore(t)
	seedAdminMetadata(ctx, t, metadata)
	objects := newAdminObjectStore(ctx, t)

	service := admin.NewService(admin.Dependencies{
		Config:   cfg,
		Metadata: metadata,
		Objects:  objects,
		Runtimes: newAdminTestRuntimes(cfg),
		Version:  build.Version("test-version"),
		Messages: newAdminMessages(t),
	})
	views, err := admin.NewTemplateEngine(newAdminMessages(t))
	if err != nil {
		t.Fatalf("new template engine: %v", err)
	}

	app := fiber.New(fiber.Config{Views: views})
	service.RegisterFiber(app)

	assertAdminResponse(t, app, "/admin", "regimux admin", "test-version", "library/node")
	assertAdminResponse(t, app, "/admin", "Committed Blob Bytes (metadata)", "KV cache backend is separate from object store")
	assertAdminResponse(t, app, "/admin", "Policy denied pulls", "Last policy denial")
	assertAdminResponse(t, app, "/admin?lang=zh", "仪表盘", "运维控制台", "English")
	assertAdminResponse(t, app, "/admin/upstreams", "Upstream Configuration", "registry-1.docker.io")
	assertAdminResponse(t, app, "/admin/pulls", "Policy Denied", "Last Policy Denial")
	assertAdminResponse(t, app, "/admin/activity", "Request Activity", "meta.pull_records", "library/node", "Policy Denied")
	assertAdminResponse(t, app, "/admin/cache", "Cache", "Committed Blob Bytes (metadata)", "recorded from committed blob metadata")
	assertAdminResponse(t, app, "/admin/storage", "Storage", "Repository Blob Links", "1.2 KiB", "Tracked Storage Bytes", "Object Store Bytes (listed)", "22 B", "1 Objects", "blob metadata size sum plus manifest object bytes recorded as metadata size", "Policy Denied")
	assertAdminResponse(t, app, "/admin/storage?lang=zh", "已落盘 Blob 字节（metadata）", "对象存储字节（list）", "1 对象", "Blob metadata 大小汇总加 manifest 对象字节")
	assertAdminResponse(t, app, "/admin/scheduler", "Prefetch Runs", "Cancel", "Retry failed")
	assertAdminResponse(t, app, "/admin/audit", "Auth Users", "alice", "hub/library/*")
	assertAdminResponse(t, app, "/admin/config", "Configuration Sources", "source metadata unavailable", "go.default.registry")
	assertAdminResponse(t, app, "/admin/partials/upstream-health", "Upstream Health", "hub")
}

func TestServiceSkipsAuthWhenRegistryAuthDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	authService, err := newTestAuthService(t, cfg)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	app := newAdminTestApp(t, cfg, authService)

	assertAdminResponse(t, app, "/admin", "regimux admin")
}

func TestServiceRequiresBasicAuthWhenRegistryAuthEnabled(t *testing.T) {
	cfg := adminAuthConfig(t)
	authService, err := newTestAuthService(t, cfg)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	app := newAdminTestApp(t, cfg, authService)

	resp := adminRequest(t, app, "/admin", "", "")
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get(fiber.HeaderWWWAuthenticate); got != `Basic realm="regimux admin"` {
		t.Fatalf("www-authenticate = %q, want admin basic challenge", got)
	}
}

func TestServiceAllowsValidBasicAuthWhenRegistryAuthEnabled(t *testing.T) {
	cfg := adminAuthConfig(t)
	authService, err := newTestAuthService(t, cfg)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	app := newAdminTestApp(t, cfg, authService)

	resp := adminRequest(t, app, "/admin", "alice", "secret")
	defer closeResponseBody(t, resp)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "regimux admin") {
		t.Fatalf("response did not contain admin layout: %s", body)
	}
}
