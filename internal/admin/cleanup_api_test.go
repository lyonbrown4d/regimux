package admin_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/admin"
)

func TestCleanupAPIReportsAsyncAcceptance(t *testing.T) {
	controller := &fakeSchedulerController{}
	app := newAdminSchedulerTestApp(t, controller)
	status, body := requestCleanupJSON(t, app)
	if status != http.StatusAccepted || !controller.cleanupCalled {
		t.Fatalf("status=%d triggered=%v body=%s", status, controller.cleanupCalled, body)
	}
	var result struct {
		Status string `json:"status"`
		Scope  string `json:"scope"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "accepted" || result.Scope != "all" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCleanupAPIReportsUnavailableWithoutLeakingControllerErrors(t *testing.T) {
	for _, controller := range []admin.SchedulerController{nil, &fakeSchedulerController{cleanupErr: errors.New("secret-internal-store-error")}} {
		app := newAdminSchedulerTestApp(t, controller)
		status, body := requestCleanupJSON(t, app)
		if status != http.StatusServiceUnavailable || !json.Valid(body) {
			t.Fatalf("status=%d body=%s", status, body)
		}
		var result map[string]string
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}
		if result["code"] == "" || result["message"] == "secret-internal-store-error" {
			t.Fatalf("unexpected error: %s", body)
		}
	}
}

func TestCleanupAPIUsesExistingAdminAuthentication(t *testing.T) {
	app := newAdminTestApp(t, adminAuthConfig(t), nil)
	status, _ := requestCleanupJSON(t, app)
	if status != http.StatusUnauthorized {
		t.Fatalf("status=%d, want admin authentication", status)
	}
}

func requestCleanupJSON(t *testing.T, app *fiber.App) (int, []byte) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/admin/scheduler/cleanup", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer closeResponseBody(t, response)
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, body
}
