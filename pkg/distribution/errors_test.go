// Package distribution_test verifies distribution responses through exported APIs.
package distribution_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestWriteError(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	distribution.WriteError(recorder, distribution.ManifestUnknown("hub/library/nginx", "not-exist"))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if got := recorder.Header().Get(distribution.HeaderDockerDistributionAPIVersion); got != distribution.APIVersion {
		t.Fatalf("api version header = %q, want %q", got, distribution.APIVersion)
	}
	if got := recorder.Header().Get(distribution.HeaderContentType); got != distribution.MediaTypeJSON {
		t.Fatalf("content type = %q, want %s", got, distribution.MediaTypeJSON)
	}

	var response distribution.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Errors) != 1 {
		t.Fatalf("errors length = %d, want 1", len(response.Errors))
	}
	err := response.Errors[0]
	if err.Code != distribution.CodeManifestUnknown || err.Message != "manifest unknown" {
		t.Fatalf("error = %+v", err)
	}
}

func TestFromErrorWrapsUnknown(t *testing.T) {
	t.Parallel()

	got := distribution.FromError(assertionError("boom"))
	if got.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", got.Status, http.StatusInternalServerError)
	}
	if got.Errors[0].Code != distribution.CodeUnknown {
		t.Fatalf("code = %s, want %s", got.Errors[0].Code, distribution.CodeUnknown)
	}
}

func TestDescriptorWithDetail(t *testing.T) {
	t.Parallel()

	got := distribution.FromError(distribution.ErrNameInvalid.WithDetail("bad path"))
	if got.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", got.Status, http.StatusBadRequest)
	}
	if got.Errors[0].Code != distribution.CodeNameInvalid || got.Errors[0].Detail != "bad path" {
		t.Fatalf("error = %+v", got.Errors[0])
	}
}

func TestDescriptorAsError(t *testing.T) {
	t.Parallel()

	got := distribution.FromError(distribution.ErrUnauthorized)
	if got.Status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", got.Status, http.StatusUnauthorized)
	}
	if got.Errors[0].Code != distribution.CodeUnauthorized {
		t.Fatalf("code = %s, want %s", got.Errors[0].Code, distribution.CodeUnauthorized)
	}
}

type assertionError string

func (e assertionError) Error() string {
	return string(e)
}
