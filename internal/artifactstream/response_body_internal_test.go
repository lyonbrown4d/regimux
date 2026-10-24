package artifactstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/transfer"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

func TestResponseBodyCloseLogsKnownLengthTruncationOnce(t *testing.T) {
	var logs bytes.Buffer
	body := newDiagnosticResponseBody(&logs, io.NopCloser(strings.NewReader("abc")), 10)

	buffer := make([]byte, 2)
	if n, err := body.Read(buffer); err != nil || n != 2 {
		t.Fatalf("Read() = (%d, %v), want (2, nil)", n, err)
	}
	closeResponseBody(t, body)
	closeResponseBody(t, body)

	entries := decodeDiagnosticEntries(t, logs.String())
	if len(entries) != 1 {
		t.Fatalf("diagnostic count = %d, want 1: %s", len(entries), logs.String())
	}
	assertDiagnosticField(t, entries[0], "error_kind", "response_closed_early")
	assertDiagnosticField(t, entries[0], "bytes_read", float64(2))
	assertDiagnosticField(t, entries[0], "expected_bytes", float64(10))
}

func TestResponseBodyDefaultLoggerLogsSafeCancellationDiagnostic(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := &responseBody{ctx: ctx, req: ResponseRequest{Body: io.NopCloser(strings.NewReader("")), Size: 20}}
	closeResponseBody(t, body)

	entries := decodeDiagnosticEntries(t, logs.String())
	if len(entries) != 1 {
		t.Fatalf("diagnostic count = %d, want 1: %s", len(entries), logs.String())
	}
	assertDiagnosticField(t, entries[0], "error_kind", "caller_canceled")
	assertDiagnosticField(t, entries[0], "caller_canceled", true)
}

func TestResponseBodyCompleteReadAndCloseDoesNotLogTruncation(t *testing.T) {
	var logs bytes.Buffer
	body := newDiagnosticResponseBody(
		&logs,
		io.NopCloser(strings.NewReader("complete")),
		int64(len("complete")),
	)
	data, err := io.ReadAll(body)
	if err != nil || string(data) != "complete" {
		t.Fatalf("ReadAll() = (%q, %v), want (%q, nil)", data, err, "complete")
	}
	closeResponseBody(t, body)
	if logs.Len() != 0 {
		t.Fatalf("complete response logged a failure: %s", logs.String())
	}
}

func TestResponseBodyReadAndCloseAreConcurrentSafe(t *testing.T) {
	var logs bytes.Buffer
	body := newDiagnosticResponseBody(
		&logs,
		io.NopCloser(strings.NewReader(strings.Repeat("x", 1024))),
		4096,
	)
	start := make(chan struct{})
	readErr := make(chan error, 1)
	closeErr := make(chan error, 1)
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		<-start
		_, err := body.Read(make([]byte, 512))
		readErr <- err
	}()
	go func() {
		defer group.Done()
		<-start
		closeErr <- body.Close()
	}()
	close(start)
	group.Wait()
	if err := <-readErr; err != nil {
		t.Fatalf("concurrent Read() error = %v", err)
	}
	if err := <-closeErr; err != nil {
		t.Fatalf("concurrent Close() error = %v", err)
	}
	closeResponseBody(t, body)

	entries := decodeDiagnosticEntries(t, logs.String())
	if len(entries) != 1 {
		t.Fatalf("diagnostic count = %d, want 1: %s", len(entries), logs.String())
	}
	assertDiagnosticField(t, entries[0], "error_kind", "response_closed_early")
}

func TestResponseBodyClassifiesTypedFailures(t *testing.T) {
	tests := []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		err  error
		want string
	}{
		{
			name: "caller cancellation priority", ctx: canceledContext,
			err: upstreamhttp.ErrBodyIdleTimeout, want: "caller_canceled",
		},
		{
			name: "caller deadline priority", ctx: expiredContext,
			err: transfer.ErrObjectOpenTimeout, want: "caller_deadline",
		},
		{
			name: "continuation failure", ctx: backgroundContext,
			err: transfer.ErrContinuationFailed, want: "continuation_failed",
		},
		{
			name: "object open timeout", ctx: backgroundContext,
			err: transfer.ErrObjectOpenTimeout, want: "object_open_timeout",
		},
		{
			name: "body idle timeout", ctx: backgroundContext,
			err: upstreamhttp.ErrBodyIdleTimeout, want: "body_idle_timeout",
		},
		{
			name: "source cancellation", ctx: backgroundContext,
			err: context.Canceled, want: "source_canceled",
		},
		{
			name: "source deadline", ctx: backgroundContext,
			err: context.DeadlineExceeded, want: "source_deadline",
		},
		{
			name: "source truncation", ctx: backgroundContext,
			err: io.ErrUnexpectedEOF, want: "truncated",
		},
		{
			name: "unclassified read error", ctx: backgroundContext,
			err: errors.New("source failed"), want: "read_error",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			ctx, cancel := test.ctx()
			t.Cleanup(cancel)
			body := &responseBody{
				ctx: ctx,
				req: ResponseRequest{
					Body:   diagnosticErrorBody{err: test.err},
					Size:   4,
					Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
				},
			}
			if _, err := body.Read(make([]byte, 4)); err == nil {
				t.Fatal("Read() error = nil, want source error")
			}
			closeResponseBody(t, body)
			entries := decodeDiagnosticEntries(t, logs.String())
			if len(entries) != 1 {
				t.Fatalf("diagnostic count = %d, want 1: %s", len(entries), logs.String())
			}
			assertDiagnosticField(t, entries[0], "error_kind", test.want)
		})
	}
}

func newDiagnosticResponseBody(logs *bytes.Buffer, body io.ReadCloser, size int64) *responseBody {
	return &responseBody{
		ctx: context.Background(),
		req: ResponseRequest{
			Body: body, Size: size, Logger: slog.New(slog.NewJSONHandler(logs, nil)),
		},
	}
}

func closeResponseBody(t *testing.T, body *responseBody) {
	t.Helper()
	if err := body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func canceledContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx, func() {}
}

func expiredContext() (context.Context, context.CancelFunc) {
	return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
}

func backgroundContext() (context.Context, context.CancelFunc) {
	return context.Background(), func() {}
}

type diagnosticErrorBody struct{ err error }

func (b diagnosticErrorBody) Read([]byte) (int, error) { return 0, b.err }

func (diagnosticErrorBody) Close() error { return nil }

func decodeDiagnosticEntries(t *testing.T, output string) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode diagnostic %q: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func assertDiagnosticField(t *testing.T, entry map[string]any, key string, want any) {
	t.Helper()
	if got := entry[key]; got != want {
		t.Fatalf("diagnostic field %q = %#v, want %#v", key, got, want)
	}
}
