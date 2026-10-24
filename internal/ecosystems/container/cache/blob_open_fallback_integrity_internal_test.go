package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocidigest "github.com/opencontainers/go-digest"
)

func TestBlobOpenFallbackReaderWithholdsBadFinalChunkAtKnownSize(t *testing.T) {
	goodBody := []byte("goodtail")
	badBody := []byte("goodFAIL")
	digest := ocidigest.FromBytes(goodBody).String()
	underlying := &chunkedFallbackBody{body: badBody, chunkSize: 4}
	var releases atomic.Int32
	reader := newKnownSizeFallbackForTest(t, goodBody, digest, underlying, func() { releases.Add(1) })

	var destination bytes.Buffer
	written, err := io.CopyN(&destination, reader, int64(len(goodBody)))
	if err == nil {
		t.Fatalf("CopyN error=nil after %d bytes; mismatched final chunk was accepted", written)
	}
	if written != 4 || destination.String() != "good" {
		t.Fatalf("CopyN delivered %q (%d bytes), want only the verified prefix", destination.Bytes(), written)
	}
	if underlying.eofReads != 0 {
		t.Fatalf("fallback read EOF %d times; exact-size validation must not depend on an extra read", underlying.eofReads)
	}

	var next [1]byte
	_, nextErr := reader.Read(next[:])
	if nextErr == nil || nextErr.Error() != err.Error() {
		t.Fatalf("read after digest failure error=%v, want stable error %v", nextErr, err)
	}
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatalf("close fallback reader: %v", closeErr)
	}
	if underlying.closes != 1 || releases.Load() != 1 {
		t.Fatalf("underlying closes=%d, permit releases=%d; want one each", underlying.closes, releases.Load())
	}
}

func TestBlobOpenFallbackReaderAcceptsExactKnownSizeWithoutEOF(t *testing.T) {
	body := []byte("goodtail")
	digest := ocidigest.FromBytes(body).String()
	underlying := &chunkedFallbackBody{body: body, chunkSize: 4}
	var releases atomic.Int32
	reader := newKnownSizeFallbackForTest(t, body, digest, underlying, func() { releases.Add(1) })

	var destination bytes.Buffer
	written, err := io.CopyN(&destination, reader, int64(len(body)))
	if err != nil || written != int64(len(body)) || !bytes.Equal(destination.Bytes(), body) {
		t.Fatalf("CopyN=(%d,%v), body=%q; want exact verified body", written, err, destination.Bytes())
	}
	if underlying.eofReads != 0 {
		t.Fatalf("fallback read EOF %d times, want zero after exact-size read", underlying.eofReads)
	}
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatalf("close fallback reader: %v", closeErr)
	}
	if underlying.closes != 1 || releases.Load() != 1 {
		t.Fatalf("underlying closes=%d, permit releases=%d; want one each", underlying.closes, releases.Load())
	}
}

func TestWrapBlobOpenFallbackRejectsWrongZeroSizeDigest(t *testing.T) {
	wrongDigest := ocidigest.FromString("nonempty").String()
	underlying := &chunkedFallbackBody{chunkSize: 4}
	var releases atomic.Int32
	req := BlobRequest{Digest: wrongDigest, Method: http.MethodGet}
	fallback := &BlobReadResult{
		Reader: underlying, Digest: wrongDigest, Size: 0, Status: http.StatusOK,
	}

	_, err := wrapBlobOpenFallback(context.Background(), req, fallback, func() { releases.Add(1) })
	if err == nil {
		t.Fatal("zero-size fallback with a non-empty digest was accepted")
	}
	digestError, ok := errors.AsType[*distribution.ErrorList](err)
	if !ok || digestError == nil {
		t.Fatalf("zero-size fallback error=%T %v, want digest validation error", err, err)
	}
	if underlying.closes != 1 || releases.Load() != 1 {
		t.Fatalf("underlying closes=%d, permit releases=%d; want one each", underlying.closes, releases.Load())
	}
}

func newKnownSizeFallbackForTest(
	t *testing.T,
	goodBody []byte,
	digest string,
	underlying *chunkedFallbackBody,
	release func(),
) *blobOpenFallbackReader {
	t.Helper()
	req := BlobRequest{Digest: digest, Method: http.MethodGet}
	fallback := &BlobReadResult{
		Reader: underlying, Digest: digest, Size: int64(len(goodBody)), Status: http.StatusOK,
	}
	result, err := wrapBlobOpenFallback(t.Context(), req, fallback, release)
	if err != nil {
		t.Fatalf("wrap fallback body: %v", err)
	}
	reader, ok := result.Reader.(*blobOpenFallbackReader)
	if !ok {
		t.Fatalf("fallback reader type=%T, want *blobOpenFallbackReader", result.Reader)
	}
	return reader
}

type chunkedFallbackBody struct {
	body      []byte
	offset    int
	chunkSize int
	eofReads  int
	closes    int
}

func (b *chunkedFallbackBody) Read(payload []byte) (int, error) {
	if b.offset == len(b.body) {
		b.eofReads++
		return 0, io.EOF
	}
	limit := b.offset + b.chunkSize
	limit = min(limit, len(b.body))
	if limit-b.offset > len(payload) {
		limit = b.offset + len(payload)
	}
	n := copy(payload, b.body[b.offset:limit])
	b.offset += n
	return n, nil
}

func (b *chunkedFallbackBody) Close() error {
	b.closes++
	return nil
}
