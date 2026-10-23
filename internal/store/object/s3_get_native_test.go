package object_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	ocidigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestS3FullGetUsesNativeResponseWithoutHead(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body []byte
	}{
		{name: "complete object", body: bytes.Repeat([]byte("native S3 complete object\n"), 4096)},
		{name: "empty object", body: []byte{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var heads, gets atomic.Int64
			digest := ocidigest.FromBytes(testCase.body).String()
			store := newNativeReadS3Store(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/bucket/"+testS3Key("native", digest), r.URL.Path)
				if r.Method == http.MethodHead {
					heads.Add(1)
					http.Error(w, "HEAD is unavailable", http.StatusForbidden)
					return
				}
				gets.Add(1)
				require.Equal(t, http.MethodGet, r.Method)
				require.Empty(t, r.Header.Get("Range"))
				writeNativeS3Object(t, w, testCase.body)
			}))
			reader, info, err := store.Get(t.Context(), digest, object.GetOptions{})
			require.NoError(t, err, "a healthy full GET must not depend on HEAD")
			payload := readNativeS3Object(t, reader)
			require.Equal(t, testCase.body, payload)
			require.Equal(t, digest, ocidigest.FromBytes(payload).String())
			require.Equal(t, digest, info.Digest)
			require.Equal(t, int64(len(testCase.body)), info.Size)
			require.Equal(t, "application/x-regimux-object", info.ContentType)
			require.Equal(t, "native-get-etag", info.ETag)
			require.Equal(t, testS3Key("native", digest), info.Path)
			require.Zero(t, heads.Load())
			require.Equal(t, int64(1), gets.Load())
		})
	}
}

func TestS3FullGetNativeNotFoundDoesNotDependOnHead(t *testing.T) {
	var heads, gets atomic.Int64
	store := newNativeReadS3Store(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			heads.Add(1)
			http.Error(w, "HEAD is unavailable", http.StatusForbidden)
			return
		}
		gets.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, err := io.WriteString(w, "<Error><Code>NoSuchKey</Code><Message>missing</Message></Error>")
		require.NoError(t, err)
	}))
	reader, info, err := store.Get(t.Context(), ocidigest.FromString("missing").String(), object.GetOptions{})
	require.ErrorIs(t, err, object.ErrNotFound)
	require.Nil(t, reader)
	require.Nil(t, info)
	require.Zero(t, heads.Load())
	require.Equal(t, int64(1), gets.Load())
}

func TestS3RangeGetKeepsNativeHeadResolution(t *testing.T) {
	for _, testCase := range []struct {
		name, requested, resolved, want string
	}{
		{name: "bounded", requested: "bytes=2-5", resolved: "bytes=2-5", want: "2345"},
		{name: "suffix", requested: "bytes=-3", resolved: "bytes=7-9", want: "789"},
		{name: "open ended", requested: "bytes=8-", resolved: "bytes=8-9", want: "89"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var heads, gets atomic.Int64
			payload := []byte("0123456789")
			store := newNativeReadS3Store(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/x-regimux-object")
				w.Header().Set("ETag", `"native-get-etag"`)
				if r.Method == http.MethodHead {
					heads.Add(1)
					w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
					return
				}
				gets.Add(1)
				require.Equal(t, testCase.resolved, r.Header.Get("Range"))
				http.ServeContent(w, r, "object", time.Unix(1, 0), bytes.NewReader(payload))
			}))
			requested, err := object.ParseRange(testCase.requested)
			require.NoError(t, err)
			reader, info, err := store.Get(t.Context(), ocidigest.FromBytes(payload).String(), object.GetOptions{Range: requested})
			require.NoError(t, err)
			require.Equal(t, testCase.want, string(readNativeS3Object(t, reader)))
			require.Equal(t, int64(len(testCase.want)), info.Size)
			require.Equal(t, "native-get-etag", info.ETag)
			require.Equal(t, int64(1), heads.Load())
			require.Equal(t, int64(1), gets.Load())
		})
	}
}

func newNativeReadS3Store(t *testing.T, handler http.Handler) *object.S3Store {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := s3.New(s3.Options{
		Region: "us-east-1", Credentials: aws.AnonymousCredentials{},
		BaseEndpoint: aws.String(server.URL), UsePathStyle: true,
		HTTPClient: server.Client(), RetryMaxAttempts: 1,
	})
	store, err := object.NewS3WithClient(client, object.S3Options{Bucket: "bucket", Prefix: "native"})
	require.NoError(t, err)
	return store
}

func readNativeS3Object(t *testing.T, reader io.ReadCloser) []byte {
	t.Helper()
	defer func() { require.NoError(t, reader.Close()) }()
	payload, err := io.ReadAll(reader)
	require.NoError(t, err)
	return payload
}

func writeNativeS3Object(t *testing.T, w http.ResponseWriter, payload []byte) {
	t.Helper()
	w.Header().Set("Content-Type", "application/x-regimux-object")
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	w.Header().Set("ETag", `"native-get-etag"`)
	_, err := w.Write(payload)
	require.NoError(t, err)
}

func TestS3FullGetNativeHonorsCanceledParent(t *testing.T) {
	var requests atomic.Int64
	store := newNativeReadS3Store(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
	}))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reader, info, err := store.Get(ctx, ocidigest.FromString("canceled").String(), object.GetOptions{})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, reader)
	require.Nil(t, info)
	require.Zero(t, requests.Load())
}

func TestS3FullGetNativeCancelsPendingRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var heads, gets atomic.Int64
	store := newNativeReadS3Store(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			heads.Add(1)
			w.Header().Set("Content-Length", "7")
			return
		}
		gets.Add(1)
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		reader, _, err := store.Get(ctx, ocidigest.FromString("pending").String(), object.GetOptions{})
		if reader != nil {
			err = errors.Join(err, reader.Close())
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("native S3 GET did not reach the fixture")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("native S3 GET ignored parent cancellation")
	}
	require.Zero(t, heads.Load())
	require.Equal(t, int64(1), gets.Load())
}
