package transfer_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/transfer"
	"github.com/stretchr/testify/require"
)

func TestHTTPRecoveryRejectsCorruptFinalChunkBeforeCopyNCompletion(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		tail      string
		lastError error
		corrupt   bool
	}{
		{name: "valid tail without EOF", tail: "ijklmnop"},
		{name: "valid tail with EOF", tail: "ijklmnop", lastError: io.EOF},
		{name: "corrupt tail without EOF", tail: "ijklmnoX", corrupt: true},
		{name: "corrupt tail with EOF", tail: "ijklmnoX", lastError: io.EOF, corrupt: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			const expected = "abcdefghijklmnop"
			const prefix = "abcdefgh"
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			deadline, _ := ctx.Deadline()
			var destination bytes.Buffer
			continuation := &httpFinalChunkBody{remaining: testCase.tail, lastError: testCase.lastError}
			opens := 0
			digest := fmt.Sprintf("%x", sha256.Sum256([]byte(expected)))
			body := transfer.HTTPRecovery(ctx, http.MethodGet,
				http.Header{"Content-Length": {"16"}, "Etag": {`"fixed"`}},
				io.NopCloser(strings.NewReader(prefix)), digest,
				func(retryCtx context.Context, headers http.Header) (*transfer.Response, error) {
					opens++
					require.Equal(t, prefix, destination.String(), "initial bytes must stream before reopening")
					require.Equal(t, "bytes=8-15", headers.Get("Range"))
					require.Equal(t, `"fixed"`, headers.Get("If-Range"))
					require.Equal(t, "identity", headers.Get("Accept-Encoding"))
					retryDeadline, ok := retryCtx.Deadline()
					require.True(t, ok)
					require.Equal(t, deadline, retryDeadline, "continuation must retain the caller budget")
					return &transfer.Response{
						Status:  http.StatusPartialContent,
						Headers: http.Header{"Content-Range": {"bytes 8-15/16"}, "Content-Length": {"8"}, "Etag": {`"fixed"`}},
						Body:    continuation, Size: 8,
					}, nil
				})
			t.Cleanup(func() { require.NoError(t, body.Close()) })

			written, err := io.CopyN(&destination, body, int64(len(expected)))
			if testCase.corrupt {
				require.ErrorContains(t, err, "SHA-256 mismatch", "CopyN must observe the corrupt final chunk")
				require.Less(t, written, int64(len(expected)), "a failed digest must not complete Content-Length")
				require.Less(t, destination.Len(), len(expected))
			} else {
				require.NoError(t, err)
				require.Equal(t, int64(len(expected)), written)
				require.Equal(t, expected, destination.String())
			}
			require.Equal(t, 1, opens)
			require.Zero(t, continuation.readsAfterEnd, "known-length completion must not require another EOF read")
		})
	}
}

type httpFinalChunkBody struct {
	remaining     string
	lastError     error
	readsAfterEnd int
}

func (b *httpFinalChunkBody) Read(payload []byte) (int, error) {
	if b.remaining == "" {
		b.readsAfterEnd++
		return 0, io.EOF
	}
	n := copy(payload, b.remaining)
	b.remaining = b.remaining[n:]
	if b.remaining == "" {
		return n, b.lastError
	}
	return n, nil
}

func (b *httpFinalChunkBody) Close() error { return nil }
