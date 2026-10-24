package artifactstream

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

func TestResponseFallbackTracksSourceReads(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		size        int64
		wantMessage string
	}{
		{name: "complete response", payload: "complete", size: int64(len("complete"))},
		{name: "truncated response", payload: "short", size: 10, wantMessage: "artifact response body length mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checkResponseFallback(t, test.payload, test.size, test.wantMessage)
		})
	}
}

func checkResponseFallback(t *testing.T, payload string, size int64, wantMessage string) {
	t.Helper()
	var logs bytes.Buffer
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	stream := Response(t.Context(), ResponseRequest{
		Status: http.StatusOK, Size: size, Body: io.NopCloser(strings.NewReader(payload)), Logger: logger,
	})
	stream(fallbackResponseContext{writer: &output})
	if output.String() != payload {
		t.Fatalf("response body = %q, want %q", output.String(), payload)
	}
	if wantMessage == "" && logs.Len() != 0 {
		t.Fatalf("complete response logged a failure: %s", logs.String())
	}
	if wantMessage != "" && !strings.Contains(logs.String(), wantMessage) {
		t.Fatalf("log = %q, want %q", logs.String(), wantMessage)
	}
}

type fallbackResponseContext struct {
	fallbackHumaContext
	writer io.Writer
}

type fallbackHumaContext = huma.Context

func (fallbackResponseContext) SetStatus(int) {}

func (ctx fallbackResponseContext) BodyWriter() io.Writer { return ctx.writer }
