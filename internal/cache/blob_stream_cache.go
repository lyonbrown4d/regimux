package cache

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/upstream"
)

func (p blobProxy) streamBlobToCache(
	ctx context.Context,
	req BlobRequest,
	resp *upstream.BlobResponse,
	mediaType string,
) io.ReadCloser {
	reader, writer := io.Pipe()
	go p.storeStreamedBlob(context.WithoutCancel(ctx), req, streamedBlob{
		reader:    reader,
		digest:    resp.Digest,
		size:      resp.Size,
		headers:   resp.Headers.Clone(),
		mediaType: mediaType,
	})
	return newBlobTeeReadCloser(resp.Body, writer, func(err error) {
		p.logBlobStreamCacheError(ctx, req, "write streamed blob to cache pipe failed", err)
	})
}

type streamedBlob struct {
	reader    *io.PipeReader
	digest    string
	size      int64
	headers   http.Header
	mediaType string
}

func (p blobProxy) storeStreamedBlob(ctx context.Context, req BlobRequest, blob streamedBlob) {
	defer func() {
		if err := blob.reader.Close(); err != nil {
			p.logBlobStreamCacheError(ctx, req, "close streamed blob cache reader failed", err)
		}
	}()

	stored, err := p.storeBlobResponse(ctx, req, &upstream.BlobResponse{
		Body:    blob.reader,
		Digest:  blob.digest,
		Size:    blob.size,
		Headers: blob.headers,
	})
	if err != nil {
		p.logBlobStreamCacheError(ctx, req, "store streamed blob cache failed", err)
		return
	}
	if err := p.upsertBlobRecords(ctx, req, stored.info, blob.mediaType); err != nil {
		p.logBlobStreamCacheError(ctx, req, "record streamed blob cache metadata failed", err)
		return
	}
	p.publishCacheStore(ctx, req, stored.info.Size, stored.info.Digest)
}

func (p blobProxy) logBlobStreamCacheError(ctx context.Context, req BlobRequest, message string, err error) {
	if p.logger == nil || err == nil {
		return
	}
	p.logger.WarnContext(ctx,
		message,
		"alias", req.UpstreamAlias,
		"repo", req.Repo,
		"digest", req.Digest,
		"error", err,
	)
}

type blobTeeReadCloser struct {
	source      io.ReadCloser
	cache       *io.PipeWriter
	onCacheFail func(error)
}

func newBlobTeeReadCloser(source io.ReadCloser, cache *io.PipeWriter, onCacheFail func(error)) io.ReadCloser {
	return &blobTeeReadCloser{
		source:      source,
		cache:       cache,
		onCacheFail: onCacheFail,
	}
}

func (r *blobTeeReadCloser) Read(buffer []byte) (int, error) {
	n, readErr := r.source.Read(buffer)
	if n > 0 {
		r.writeCache(buffer[:n])
	}
	if readErr != nil {
		r.closeCache(readErr)
	}
	return n, blobSourceReadError(readErr)
}

func (r *blobTeeReadCloser) Close() error {
	r.closeCache(io.ErrClosedPipe)
	if err := r.source.Close(); err != nil {
		return wrapError(err, "close streamed blob source")
	}
	return nil
}

func (r *blobTeeReadCloser) writeCache(data []byte) {
	if r.cache == nil {
		return
	}
	if _, err := r.cache.Write(data); err != nil {
		r.failCache(err)
	}
}

func (r *blobTeeReadCloser) closeCache(err error) {
	if r.cache == nil {
		return
	}
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if closeErr := r.cache.CloseWithError(err); closeErr != nil {
		r.failCache(closeErr)
		return
	}
	r.cache = nil
}

func (r *blobTeeReadCloser) failCache(err error) {
	if r.cache != nil {
		if closeErr := r.cache.CloseWithError(err); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		r.cache = nil
	}
	if r.onCacheFail != nil {
		r.onCacheFail(err)
	}
}

func blobSourceReadError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	return wrapError(err, "read streamed blob source")
}
