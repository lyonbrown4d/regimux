package admin_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arcgolabs/authx"
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/admin"
	"github.com/lyonbrown4d/regimux/internal/auth"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
	containerauth "github.com/lyonbrown4d/regimux/internal/ecosystems/container/auth"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocidigest "github.com/opencontainers/go-digest"
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

func closeResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp == nil || resp.Body == nil {
		return
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
}

func newAdminTestApp(t *testing.T, cfg config.Config, authService *auth.Service) *fiber.App {
	t.Helper()

	ctx := context.Background()
	metadata := newMetadataStore(t)
	seedAdminMetadata(ctx, t, metadata)

	service := admin.NewService(admin.Dependencies{
		Config:   cfg,
		Metadata: metadata,
		Runtimes: newAdminTestRuntimes(cfg),
		Version:  build.Version("test-version"),
		Auth:     authService,
		Messages: newAdminMessages(t),
	})
	views, err := admin.NewTemplateEngine(newAdminMessages(t))
	if err != nil {
		t.Fatalf("new template engine: %v", err)
	}

	app := fiber.New(fiber.Config{Views: views})
	service.RegisterFiber(app)
	return app
}

func newAdminMessages(t *testing.T) *admin.Messages {
	t.Helper()
	messages, err := admin.NewMessages()
	if err != nil {
		t.Fatalf("new admin messages: %v", err)
	}
	return messages
}

func newTestAuthService(t *testing.T, cfg config.Config) (*auth.Service, error) {
	t.Helper()
	users := auth.NewUserDirectory(cfg.Auth)
	providers := collectionlist.NewList[authx.AuthenticationProvider](
		auth.NewBasicAuthenticationProvider(users),
		auth.NewJWTAuthenticationProvider(cfg.Auth),
	)
	resolvers := collectionlist.NewList[auth.ResourceResolver]()
	resolvers.Add(containerauth.NewResourceResolver(cfg))
	service, err := auth.NewService(cfg, nil, users, providers, resolvers)
	if err != nil {
		return nil, fmt.Errorf("new auth service: %w", err)
	}
	return service, nil
}

func adminAuthConfig(t *testing.T) config.Config {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.Auth = config.RegistryAuthConfig{
		Enabled:     true,
		Service:     "regimux",
		Issuer:      "regimux",
		TokenSecret: "test-secret",
		Users: map[string]config.AuthUserConfig{
			"alice": {Password: "secret"},
		},
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	return cfg
}

func adminRequest(t *testing.T, app *fiber.App, path, username, password string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://regimux.test"+path, http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("test request %s: %v", path, err)
	}
	return resp
}

func newMetadataStore(t *testing.T) *meta.SQLStore {
	t.Helper()
	store, err := meta.OpenSQLite(filepath.Join(t.TempDir(), "regimux.db"), nil)
	if err != nil {
		t.Fatalf("open metadata store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close metadata store: %v", err)
		}
	})
	return store
}

func newAdminObjectStore(ctx context.Context, t *testing.T) object.Store {
	t.Helper()
	store, err := object.NewMemory("admin-test-objects")
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}
	body := []byte("admin object list body")
	if _, err := store.Put(ctx, digestForBody(body), bytes.NewReader(body), object.PutOptions{
		ContentType: distribution.MediaTypeOctetStream,
	}); err != nil {
		t.Fatalf("put object: %v", err)
	}
	return store
}

func digestForBody(body []byte) string {
	sum := sha256.Sum256(body)
	return ocidigest.SHA256.String() + ":" + hex.EncodeToString(sum[:])
}

func seedAdminMetadata(ctx context.Context, t *testing.T, store meta.Store) {
	t.Helper()
	now := time.Now().UTC()
	key := meta.PullKey{Alias: "hub", Repository: "library/node", Reference: "20"}
	if _, err := store.RecordPull(ctx, key, now.Add(-time.Minute)); err != nil {
		t.Fatalf("record pull: %v", err)
	}
	if _, err := store.RecordUpstreamPull(ctx, key, now.Add(-30*time.Second)); err != nil {
		t.Fatalf("record upstream pull: %v", err)
	}
	if _, err := store.RecordPolicyDeniedPull(ctx, key, now.Add(-15*time.Second)); err != nil {
		t.Fatalf("record policy denied pull: %v", err)
	}
	if _, err := store.UpsertBlob(ctx, meta.BlobRecord{
		Digest:       "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Size:         1234,
		MediaType:    distribution.MediaTypeOctetStream,
		LastAccessAt: now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("upsert blob: %v", err)
	}
	if _, err := store.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:          "hub",
		Repository:     "library/node",
		Digest:         "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SourceManifest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		LastAccessAt:   now,
		LastVerifiedAt: now,
	}); err != nil {
		t.Fatalf("upsert repo blob: %v", err)
	}
	if _, err := store.UpsertManifest(ctx, meta.ManifestRecord{
		Alias:      "hub",
		Repository: "library/node",
		Digest:     "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		MediaType:  distribution.MediaTypeOCIManifest,
		Size:       321,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("upsert manifest: %v", err)
	}
}

func assertAdminResponse(t *testing.T, app *fiber.App, path string, contains ...string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://regimux.test"+path, http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("test request %s: %v", path, err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d for %s: %s", resp.StatusCode, path, body)
	}
	text := string(body)
	for _, value := range contains {
		if !strings.Contains(text, value) {
			t.Fatalf("response %s did not contain %q: %s", path, value, text)
		}
	}
}
