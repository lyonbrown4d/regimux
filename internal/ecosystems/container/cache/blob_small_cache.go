package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocidigest "github.com/opencontainers/go-digest"
)

const smallBlobCacheVersion = 1

type smallBlobCacheConfig struct {
	enabled      bool
	maxSizeBytes int64
	ttl          time.Duration
}

type smallBlobCacheEnvelope struct {
	Version   int    `json:"version"`
	Digest    string `json:"digest"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
	Body      []byte `json:"body"`
}

func (p blobProxy) lookupSmallBlobCache(ctx context.Context, req BlobRequest) (*BlobReadResult, bool, error) {
	if !p.canUseSmallBlobCache(req) {
		return nil, false, nil
	}
	data, ok, err := p.cache.Get(ctx, smallBlobCacheKey(req.Digest))
	if err != nil {
		return nil, false, wrapError(err, "get small blob cache")
	}
	if !ok {
		return nil, false, nil
	}

	envelope, err := decodeSmallBlobCache(data)
	if err != nil || !validSmallBlobCache(req.Digest, envelope) {
		p.evictSmallBlobCache(ctx, req.Digest, err)
		return nil, false, nil
	}
	result, err := smallBlobCacheResult(req, envelope)
	if err != nil {
		return nil, false, err
	}
	p.logBlobCacheHit(ctx, req, "small_blob_cache_hit")
	return result, true, nil
}

func (p blobProxy) storeSmallBlobCache(ctx context.Context, digest, mediaType string, infoSize int64, body []byte) {
	if !p.canStoreSmallBlobCache(infoSize, body) {
		return
	}
	data, err := encodeSmallBlobCache(digest, mediaType, body)
	if err != nil {
		p.logSmallBlobCacheError(ctx, digest, err)
		return
	}
	if err := p.cache.Set(ctx, smallBlobCacheKey(digest), data, p.smallCache.ttl); err != nil {
		p.logSmallBlobCacheError(ctx, digest, err)
	}
}

func (p blobProxy) canUseSmallBlobCache(req BlobRequest) bool {
	return p.smallCache.enabled &&
		p.cache != nil &&
		req.Digest != "" &&
		(req.Method == "" || req.Method == http.MethodGet || req.Method == http.MethodHead)
}

func (p blobProxy) canStoreSmallBlobCache(infoSize int64, body []byte) bool {
	return p.smallCache.enabled &&
		p.cache != nil &&
		p.smallCache.maxSizeBytes > 0 &&
		infoSize >= 0 &&
		infoSize <= p.smallCache.maxSizeBytes &&
		int64(len(body)) == infoSize
}

func (p blobProxy) evictSmallBlobCache(ctx context.Context, digest string, cause error) {
	if deleteErr := p.cache.Delete(ctx, smallBlobCacheKey(digest)); deleteErr != nil {
		p.logSmallBlobCacheError(ctx, digest, deleteErr)
		return
	}
	if cause != nil {
		p.logSmallBlobCacheError(ctx, digest, cause)
	}
}

func (p blobProxy) logSmallBlobCacheError(ctx context.Context, digest string, err error) {
	if p.logger == nil || err == nil {
		return
	}
	p.logger.DebugContext(ctx, "small blob cache skipped", "digest", digest, "error", err)
}

func smallBlobCacheKey(digest string) string {
	return "blob-small:" + digest
}

func encodeSmallBlobCache(digest, mediaType string, body []byte) ([]byte, error) {
	envelope := smallBlobCacheEnvelope{
		Version:   smallBlobCacheVersion,
		Digest:    digest,
		MediaType: mediaType,
		Size:      int64(len(body)),
		Body:      body,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, wrapError(err, "marshal small blob cache")
	}
	return data, nil
}

func decodeSmallBlobCache(data []byte) (smallBlobCacheEnvelope, error) {
	var envelope smallBlobCacheEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return smallBlobCacheEnvelope{}, wrapError(err, "decode small blob cache")
	}
	return envelope, nil
}

func validSmallBlobCache(expectedDigest string, envelope smallBlobCacheEnvelope) bool {
	if envelope.Version != smallBlobCacheVersion || envelope.Digest != expectedDigest {
		return false
	}
	if envelope.Size != int64(len(envelope.Body)) {
		return false
	}
	digest, err := ocidigest.Parse(expectedDigest)
	if err != nil {
		return false
	}
	verifier := digest.Verifier()
	if _, err := verifier.Write(envelope.Body); err != nil {
		return false
	}
	return verifier.Verified()
}

func smallBlobCacheResult(req BlobRequest, envelope smallBlobCacheEnvelope) (*BlobReadResult, error) {
	headers := smallBlobHeaders(envelope)
	status, size, _, err := blobReadOptions(req, envelope.Size, headers)
	if err != nil {
		return nil, err
	}
	body, err := smallBlobBodyForRequest(req, envelope.Body)
	if err != nil {
		return nil, err
	}
	return &BlobReadResult{
		Reader:  io.NopCloser(bytes.NewReader(body)),
		Digest:  envelope.Digest,
		Size:    size,
		Range:   req.Range,
		Status:  status,
		Headers: headers,
		Cache:   CacheHit,
	}, nil
}

func smallBlobHeaders(envelope smallBlobCacheEnvelope) http.Header {
	headers := http.Header{}
	headers.Set(distribution.HeaderContentLength, strconv.FormatInt(envelope.Size, 10))
	headers.Set(distribution.HeaderETag, envelope.Digest)
	if envelope.MediaType != "" {
		headers.Set(distribution.HeaderContentType, envelope.MediaType)
	}
	return headers
}

func smallBlobBodyForRequest(req BlobRequest, body []byte) ([]byte, error) {
	if req.Method == http.MethodHead {
		return nil, nil
	}
	if req.Range == nil {
		return body, nil
	}
	resolved, err := req.Range.Resolve(int64(len(body)))
	if err != nil {
		return nil, distribution.ErrRangeInvalid.WithDetail(err.Error())
	}
	return body[resolved.Start : resolved.End+1], nil
}

type smallBlobRecorder struct {
	buffer   bytes.Buffer
	limit    int64
	overflow bool
}

func newSmallBlobRecorder(limit int64) *smallBlobRecorder {
	return &smallBlobRecorder{limit: limit}
}

func (r *smallBlobRecorder) Write(data []byte) (int, error) {
	written := len(data)
	if r == nil || r.limit <= 0 {
		return written, nil
	}
	remaining := r.limit - int64(r.buffer.Len())
	if remaining <= 0 {
		r.overflow = true
		return written, nil
	}
	if int64(len(data)) > remaining {
		r.overflow = true
		data = data[:remaining]
	}
	if _, err := r.buffer.Write(data); err != nil {
		return 0, wrapError(err, "record small blob body")
	}
	return written, nil
}

func (r *smallBlobRecorder) Bytes(infoSize int64) []byte {
	if r == nil || r.overflow || infoSize < 0 || int64(r.buffer.Len()) != infoSize {
		return nil
	}
	return r.buffer.Bytes()
}

func smallBlobRecordingReader(reader io.Reader, recorder *smallBlobRecorder) io.Reader {
	if recorder == nil {
		return reader
	}
	return io.TeeReader(reader, recorder)
}
