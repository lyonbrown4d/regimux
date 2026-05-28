package admin_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/admin"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestServiceRendersSyncPage(t *testing.T) {
	app, _ := newAdminSyncTestApp(t, &fakeManualSyncer{})

	assertAdminResponse(t, app, "/admin/sync", "Manual Sync", "gitlab/gitlab-ce", "Sync now")
}

func TestServiceSyncSubmitValidatesRepository(t *testing.T) {
	app, _ := newAdminSyncTestApp(t, &fakeManualSyncer{})

	resp, body := adminPostForm(t, app, "/admin/sync", url.Values{
		"upstream_alias": {"hub"},
	})
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "repository is required") {
		t.Fatalf("response did not contain validation error: %s", body)
	}
}

func TestServiceSyncSubmitCallsSyncer(t *testing.T) {
	syncer := &fakeManualSyncer{}
	app, fake := newAdminSyncTestApp(t, syncer)

	resp, body := adminPostForm(t, app, "/admin/sync", url.Values{
		"upstream_alias": {"hub"},
		"repository":     {"library/node:20"},
	})
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	if !fake.called {
		t.Fatal("syncer was not called")
	}
	if fake.opts.Alias != "hub" || fake.opts.Repo != "library/node" || fake.opts.Reference != "20" {
		t.Fatalf("sync options = %+v", fake.opts)
	}
	for _, value := range []string{
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"library/node",
		"20",
		"application/vnd.oci.image.manifest.v1",
	} {
		if !strings.Contains(body, value) {
			t.Fatalf("response did not contain %q: %s", value, body)
		}
	}
}

func newAdminSyncTestApp(t *testing.T, syncer *fakeManualSyncer) (*fiber.App, *fakeManualSyncer) {
	t.Helper()

	ctx := context.Background()
	cfg := config.DefaultConfig()
	metadata := newMetadataStore(t)
	seedAdminMetadata(ctx, t, metadata)

	service := admin.NewService(admin.Dependencies{
		Config:   cfg,
		Metadata: metadata,
		Upstream: upstream.NewClientFromConfigs(upstream.ConfigsFromUpstreamConfigs(cfg.OrderedUpstreams()), nil, nil, nil),
		Version:  build.Version("test-version"),
		Messages: newAdminMessages(t),
		Syncer:   syncer,
	})
	views, err := admin.NewTemplateEngine(newAdminMessages(t))
	if err != nil {
		t.Fatalf("new template engine: %v", err)
	}

	app := fiber.New(fiber.Config{Views: views})
	service.RegisterFiber(app)
	return app, syncer
}

func adminPostForm(t *testing.T, app *fiber.App, path string, form url.Values) (*http.Response, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://regimux.test"+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationForm)
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
	return resp, string(body)
}

type fakeManualSyncer struct {
	called bool
	opts   prefetch.SyncOptions
	err    error
}

func (f *fakeManualSyncer) Sync(_ context.Context, opts prefetch.SyncOptions) (*prefetch.SyncReport, error) {
	f.called = true
	f.opts = opts
	if f.err != nil {
		return nil, f.err
	}
	return &prefetch.SyncReport{
		Alias:              opts.Alias,
		Repo:               opts.Repo,
		Reference:          opts.Reference,
		ManifestDigest:     "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		MediaType:          distribution.MediaTypeOCIManifest,
		LayerCount:         3,
		BlobCount:          4,
		ChildManifestCount: 0,
		Duration:           1500 * time.Millisecond,
	}, nil
}
