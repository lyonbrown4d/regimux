package transfer_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/transfer"
	"github.com/stretchr/testify/require"
)

func TestHTTPRecoveryValidatesContinuationBeforeAppending(t *testing.T) {
	for _, change := range []string{"none", "status", "start", "total", "etag", "encoding", "length", "digest"} {
		t.Run(change, func(t *testing.T) {
			response := &transfer.Response{
				Status:  http.StatusPartialContent,
				Headers: http.Header{"Content-Range": {"bytes 3-7/8"}, "Content-Length": {"5"}, "Etag": {`"fixed"`}},
				Body:    io.NopCloser(strings.NewReader("defgh")), Size: 5,
			}
			modifyContinuation(response, change)
			var requests int
			expected := fmt.Sprintf("%x", sha256.Sum256([]byte("abcdefgh")))
			body := transfer.HTTPRecovery(t.Context(), http.MethodGet,
				http.Header{"Content-Length": {"8"}, "Etag": {`"fixed"`}},
				io.NopCloser(strings.NewReader("abc")), expected,
				func(_ context.Context, headers http.Header) (*transfer.Response, error) {
					requests++
					require.Equal(t, "bytes=3-7", headers.Get("Range"))
					require.Equal(t, `"fixed"`, headers.Get("If-Range"))
					return response, nil
				})
			actual, err := io.ReadAll(body)
			if change == "none" {
				require.NoError(t, err)
				require.Equal(t, "abcdefgh", string(actual))
				require.Equal(t, 1, requests)
			} else {
				require.Error(t, err, "accepted unsafe continuation %s: body=%q", change, actual)
			}
			require.NoError(t, body.Close())
		})
	}
}

func modifyContinuation(response *transfer.Response, change string) {
	switch change {
	case "status":
		response.Status = http.StatusOK
	case "start":
		response.Headers.Set("Content-Range", "bytes 2-7/8")
	case "total":
		response.Headers.Set("Content-Range", "bytes 3-7/9")
	case "etag":
		response.Headers.Set("ETag", `"changed"`)
	case "encoding":
		response.Headers.Set("Content-Encoding", "gzip")
	case "length":
		response.Headers.Set("Content-Length", "6")
	case "digest":
		response.Body = io.NopCloser(strings.NewReader("XXXXX"))
	}
}

func TestHTTPRecoveryRetainsOriginalPartialRange(t *testing.T) {
	var requested string
	body := transfer.HTTPRecovery(t.Context(), http.MethodGet,
		http.Header{"Content-Range": {"bytes 2-7/10"}, "Content-Length": {"6"}, "Etag": {`"fixed"`}},
		io.NopCloser(strings.NewReader("23")), "",
		func(_ context.Context, headers http.Header) (*transfer.Response, error) {
			requested = headers.Get("Range")
			return &transfer.Response{
				Status:  http.StatusPartialContent,
				Headers: http.Header{"Content-Range": {"bytes 4-7/10"}, "Content-Length": {"4"}, "Etag": {`"fixed"`}},
				Body:    io.NopCloser(strings.NewReader("4567")), Size: 4,
			}, nil
		})
	actual, err := io.ReadAll(body)
	if err != nil || string(actual) != "234567" || requested != "bytes=4-7" {
		t.Fatalf("body=%q range=%q error=%v", actual, requested, err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPRecoveryRequiresStableIdentityAndLength(t *testing.T) {
	for _, headers := range []http.Header{
		{"Content-Length": {"8"}},
		{"Content-Length": {"8"}, "Etag": {`W/"weak"`}},
		{"Etag": {`"strong"`}},
		{"Content-Length": {"8"}, "Etag": {`"strong"`}, "Content-Encoding": {"gzip"}},
	} {
		body := transfer.HTTPRecovery(t.Context(), http.MethodGet, headers,
			io.NopCloser(strings.NewReader("abc")), "",
			func(context.Context, http.Header) (*transfer.Response, error) {
				t.Error("unverifiable representation was resumed")
				return nil, errors.New("unverifiable representation must not resume")
			})
		if _, err := io.ReadAll(body); err != nil {
			t.Fatal(err)
		}
		if err := body.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHTTPRecoveryPartialDigestWithoutStrongETagDoesNotResume(t *testing.T) {
	for _, etag := range []string{"", `W/"weak"`} {
		t.Run("etag="+etag, func(t *testing.T) {
			var opens int
			expected := fmt.Sprintf("%x", sha256.Sum256([]byte("0123456789")))
			body := transfer.HTTPRecovery(t.Context(), http.MethodGet,
				http.Header{"Content-Range": {"bytes 2-7/10"}, "Content-Length": {"6"}, "Etag": {etag}},
				io.NopCloser(strings.NewReader("23")), expected,
				func(_ context.Context, _ http.Header) (*transfer.Response, error) {
					opens++
					return &transfer.Response{
						Status:  http.StatusPartialContent,
						Headers: http.Header{"Content-Range": {"bytes 4-7/10"}, "Content-Length": {"4"}, "Etag": {etag}},
						Body:    io.NopCloser(strings.NewReader("XXXX")), Size: 4,
					}, nil
				})
			actual, err := io.ReadAll(body)
			require.NoError(t, err)
			require.NoError(t, body.Close())
			require.Equal(t, 0, opens, "whole-file SHA cannot validate bytes belonging to a partial range")
			require.Equal(t, "23", string(actual), "unverified continuation was appended")
		})
	}
}
