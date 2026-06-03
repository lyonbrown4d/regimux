package admin_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/admin"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/upstream"
)

func TestServiceSchedulerCleanupSubmitTriggersCleanup(t *testing.T) {
	scheduler := &fakeSchedulerController{}
	app := newAdminSchedulerTestApp(t, scheduler)

	resp, body := adminPostForm(t, app, "/admin/scheduler/cleanup", url.Values{})
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, http.StatusOK, body)
	}
	if !scheduler.cleanupCalled {
		t.Fatal("cleanup was not triggered")
	}
	if !strings.Contains(body, "Cleanup task has been submitted.") {
		t.Fatalf("response did not contain cleanup success message: %s", body)
	}
}

func TestServiceSchedulerCleanupSubmitRequiresController(t *testing.T) {
	app := newAdminSchedulerTestApp(t, nil)

	resp, body := adminPostForm(t, app, "/admin/scheduler/cleanup", url.Values{})
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, http.StatusServiceUnavailable, body)
	}
	if !strings.Contains(body, "Cleanup control is not configured") {
		t.Fatalf("response did not contain unavailable message: %s", body)
	}
}

func TestServiceSchedulerCleanupSubmitReturnsControllerError(t *testing.T) {
	scheduler := &fakeSchedulerController{cleanupErr: errors.New("cleanup unavailable")}
	app := newAdminSchedulerTestApp(t, scheduler)

	resp, body := adminPostForm(t, app, "/admin/scheduler/cleanup", url.Values{})
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, http.StatusBadGateway, body)
	}
	if !strings.Contains(body, "cleanup unavailable") {
		t.Fatalf("response did not contain cleanup error: %s", body)
	}
}

func TestServiceSchedulerProbeSubmitValidatesForm(t *testing.T) {
	app := newAdminSchedulerTestApp(t, &fakeSchedulerController{})
	resp, body := adminPostForm(t, app, "/admin/scheduler/probe", url.Values{})
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, http.StatusBadRequest, body)
	}
	if !strings.Contains(body, "Probe ecosystem is required") {
		t.Fatalf("response did not contain ecosystem validation message: %s", body)
	}

	resp, body = adminPostForm(t, app, "/admin/scheduler/probe", url.Values{
		"ecosystem": {"container"},
	})
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, http.StatusBadRequest, body)
	}
	if !strings.Contains(body, "Probe alias is required") {
		t.Fatalf("response did not contain alias validation message: %s", body)
	}
}

func TestServiceSchedulerProbeSubmitTriggersProbe(t *testing.T) {
	scheduler := &fakeSchedulerController{}
	app := newAdminSchedulerTestApp(t, scheduler)

	resp, body := adminPostForm(t, app, "/admin/scheduler/probe", url.Values{
		"ecosystem": {"container"},
		"alias":     {"hub"},
	})
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, http.StatusOK, body)
	}
	if !scheduler.probeCalled {
		t.Fatal("probe was not triggered")
	}
	if scheduler.probeEcosystem != "container" || scheduler.probeAlias != "hub" {
		t.Fatalf("probe target = %s:%s, want container:hub", scheduler.probeEcosystem, scheduler.probeAlias)
	}
	if !strings.Contains(body, "Probe task has been submitted.") {
		t.Fatalf("response did not contain probe success message: %s", body)
	}
}

func TestServiceSchedulerProbeSubmitRequiresController(t *testing.T) {
	app := newAdminSchedulerTestApp(t, nil)

	resp, body := adminPostForm(t, app, "/admin/scheduler/probe", url.Values{
		"ecosystem": {"container"},
		"alias":     {"hub"},
	})
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, http.StatusServiceUnavailable, body)
	}
	if !strings.Contains(body, "Probe control is not configured") {
		t.Fatalf("response did not contain unavailable message: %s", body)
	}
}

func TestServiceSchedulerProbeSubmitReturnsControllerError(t *testing.T) {
	scheduler := &fakeSchedulerController{probeErr: errors.New("probe unavailable")}
	app := newAdminSchedulerTestApp(t, scheduler)

	resp, body := adminPostForm(t, app, "/admin/scheduler/probe", url.Values{
		"ecosystem": {"container"},
		"alias":     {"hub"},
	})
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, http.StatusBadGateway, body)
	}
	if !strings.Contains(body, "probe unavailable") {
		t.Fatalf("response did not contain probe error: %s", body)
	}
}

func newAdminSchedulerTestApp(t *testing.T, schedulerController admin.SchedulerController) *fiber.App {
	t.Helper()

	cfg := config.DefaultConfig()
	metadata := newMetadataStore(t)
	seedAdminMetadata(context.Background(), t, metadata)

	service := admin.NewService(admin.Dependencies{
		Config:    cfg,
		Metadata:  metadata,
		Upstream:  upstream.NewClientFromConfigs(upstream.ConfigsFromUpstreamConfigs(cfg.OrderedContainerUpstreams()), nil, nil, nil),
		Version:   build.Version("test-version"),
		Messages:  newAdminMessages(t),
		Scheduler: schedulerController,
	})

	views, err := admin.NewTemplateEngine(newAdminMessages(t))
	if err != nil {
		t.Fatalf("new template engine: %v", err)
	}

	app := fiber.New(fiber.Config{Views: views})
	service.RegisterFiber(app)
	return app
}

type fakeSchedulerController struct {
	cleanupCalled bool
	cleanupErr    error

	probeCalled    bool
	probeErr       error
	probeEcosystem string
	probeAlias     string
}

func (f *fakeSchedulerController) TriggerCleanup(_ context.Context) error {
	f.cleanupCalled = true
	return f.cleanupErr
}

func (f *fakeSchedulerController) TriggerProbe(_ context.Context, ecosystemName, alias string) error {
	f.probeCalled = true
	f.probeEcosystem = ecosystemName
	f.probeAlias = alias
	return f.probeErr
}
