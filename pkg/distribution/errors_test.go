package distribution

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteError(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	WriteError(recorder, ManifestUnknown("hub/library/nginx", "not-exist"))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if got := recorder.Header().Get("Docker-Distribution-Api-Version"); got != APIVersion {
		t.Fatalf("api version header = %q, want %q", got, APIVersion)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q, want application/json", got)
	}

	var response ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Errors) != 1 {
		t.Fatalf("errors length = %d, want 1", len(response.Errors))
	}
	err := response.Errors[0]
	if err.Code != CodeManifestUnknown || err.Message != "manifest unknown" {
		t.Fatalf("error = %+v", err)
	}
}

func TestFromErrorWrapsUnknown(t *testing.T) {
	t.Parallel()

	got := FromError(assertionError("boom"))
	if got.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", got.Status, http.StatusInternalServerError)
	}
	if got.Errors[0].Code != CodeUnknown {
		t.Fatalf("code = %s, want %s", got.Errors[0].Code, CodeUnknown)
	}
}

func TestDescriptorWithDetail(t *testing.T) {
	t.Parallel()

	got := FromError(ErrNameInvalid.WithDetail("bad path"))
	if got.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", got.Status, http.StatusBadRequest)
	}
	if got.Errors[0].Code != CodeNameInvalid || got.Errors[0].Detail != "bad path" {
		t.Fatalf("error = %+v", got.Errors[0])
	}
}

func TestDescriptorAsError(t *testing.T) {
	t.Parallel()

	got := FromError(ErrUnauthorized)
	if got.Status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", got.Status, http.StatusUnauthorized)
	}
	if got.Errors[0].Code != CodeUnauthorized {
		t.Fatalf("code = %s, want %s", got.Errors[0].Code, CodeUnauthorized)
	}
}

type assertionError string

func (e assertionError) Error() string {
	return string(e)
}
