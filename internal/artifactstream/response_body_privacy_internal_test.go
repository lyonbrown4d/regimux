package artifactstream

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestResponseBodyNeverLogsSignedSourceURL(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(strconv.FormatBool(explicit), func(t *testing.T) {
			assertSignedSourceURLIsRedacted(t, explicit)
		})
	}
}

func assertSignedSourceURLIsRedacted(t *testing.T, explicit bool) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	previous := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })

	query := url.Values{}
	query.Set("signature", strings.Repeat("x", 24))
	sourceURL := (&url.URL{
		Scheme:   "https",
		Host:     "private.invalid",
		Path:     "/object",
		RawQuery: query.Encode(),
	}).String()
	req := ResponseRequest{
		Size: 4,
		Body: diagnosticErrorBody{err: &url.Error{
			Op: "Get", URL: sourceURL, Err: io.ErrUnexpectedEOF,
		}},
	}
	if explicit {
		req.Logger = logger
	}
	body := &responseBody{ctx: context.Background(), req: req}
	if _, err := body.Read(make([]byte, 4)); err == nil {
		t.Fatal("Read() error = nil, want wrapped source failure")
	}
	closeResponseBody(t, body)

	entries := decodeDiagnosticEntries(t, logs.String())
	if len(entries) != 1 {
		t.Fatalf("diagnostic count = %d, want 1: %s", len(entries), logs.String())
	}
	assertDiagnosticField(t, entries[0], "error_kind", "truncated")
	if strings.Contains(logs.String(), sourceURL) ||
		strings.Contains(logs.String(), query.Get("signature")) ||
		strings.Contains(logs.String(), "private.invalid") {
		t.Fatalf("diagnostic leaked signed source URL: %s", logs.String())
	}
}
