package upstream_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func requireNoError(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
}

func requireEqual[T comparable](t *testing.T, got, want T, label string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response JSON: %v", err)
	}
}

func writeString(t *testing.T, w http.ResponseWriter, value string) {
	t.Helper()
	if _, err := io.WriteString(w, value); err != nil {
		t.Fatalf("write response body: %v", err)
	}
}

func closeBody(t *testing.T, body io.Closer) {
	t.Helper()
	if err := body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
}

func readAndClose(t *testing.T, body io.ReadCloser) string {
	t.Helper()
	data, err := io.ReadAll(body)
	requireNoError(t, err, "read response body")
	closeBody(t, body)
	return string(data)
}
