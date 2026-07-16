package artifactstream_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/artifactstream"
)

func TestCopyLogsReadFailureWithArtifactContext(t *testing.T) {
	t.Parallel()

	readErr := errors.New("storage read timeout")
	source := &failingReader{payload: []byte("part"), err: readErr}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	written := artifactstream.Copy(context.Background(), artifactstream.CopyRequest{
		Destination:   io.Discard,
		Source:        source,
		Logger:        logger,
		Ecosystem:     "maven",
		Alias:         "central",
		Reference:     "org/example/demo/1.0/demo-1.0.pom",
		Cache:         "hit",
		ExpectedBytes: 128,
	})

	if written != 4 {
		t.Fatalf("written = %d, want 4", written)
	}
	for _, value := range []string{
		"artifact response stream failed",
		"storage read timeout",
		"\"ecosystem\":\"maven\"",
		"\"alias\":\"central\"",
		"\"bytes_written\":4",
		"\"expected_bytes\":128",
	} {
		if !strings.Contains(logs.String(), value) {
			t.Fatalf("log %q does not contain %q", logs.String(), value)
		}
	}
}

func TestCopyLogsLengthMismatch(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	written := artifactstream.Copy(context.Background(), artifactstream.CopyRequest{
		Destination:   io.Discard,
		Source:        strings.NewReader("short"),
		Logger:        logger,
		ExpectedBytes: 10,
	})

	if written != 5 {
		t.Fatalf("written = %d, want 5", written)
	}
	if !strings.Contains(logs.String(), "artifact response body length mismatch") {
		t.Fatalf("log = %q, want length mismatch", logs.String())
	}
}

type failingReader struct {
	payload []byte
	err     error
	read    bool
}

func (r *failingReader) Read(buffer []byte) (int, error) {
	if !r.read {
		r.read = true
		return copy(buffer, r.payload), nil
	}
	return 0, r.err
}
