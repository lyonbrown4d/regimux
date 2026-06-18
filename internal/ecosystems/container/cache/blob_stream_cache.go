package cache

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
)

var (
	errBlobStreamSchedulerSaturated   = errors.New("blob stream scheduler saturated")
	errBlobStreamSchedulerUnavailable = errors.New("blob stream scheduler unavailable")
)

func (p blobProxy) streamBlobToCache(
	ctx context.Context,
	req BlobRequest,
	resp *upstream.BlobResponse,
	mediaType string,
	onDone func(error),
) io.ReadCloser {
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	if err := p.submitStreamedBlobCache(func() {
		err := p.storeStreamedBlob(context.WithoutCancel(ctx), req, streamedBlob{
			reader:    reader,
			digest:    resp.Digest,
			size:      resp.Size,
			headers:   resp.Headers.Clone(),
			mediaType: mediaType,
		})
		if onDone != nil {
			onDone(err)
		}
		done <- err
	}); err != nil {
		p.closeStreamedBlobPipeAfterSchedulerFailure(ctx, req, reader, writer, err)
		p.publishStreamCacheFallback(ctx, req, streamCacheFallbackReason(err))
		if errors.Is(err, errBlobStreamSchedulerSaturated) {
			p.publishStreamCacheFill(ctx, req, "saturated", "scheduler_saturated")
		} else {
			p.publishStreamCacheFill(ctx, req, "skipped", streamCacheFallbackReason(err))
		}
		p.logBlobStreamCacheError(ctx, req, "submit streamed blob cache failed", err)
		if onDone != nil {
			onDone(err)
		}
		return resp.Body
	}
	return newBlobTeeReadCloser(resp.Body, writer, done, func(err error) {
		p.logBlobStreamCacheError(ctx, req, "write streamed blob to cache pipe failed", err)
	})
}

func (p blobProxy) closeStreamedBlobPipeAfterSchedulerFailure(
	ctx context.Context,
	req BlobRequest,
	reader *io.PipeReader,
	writer *io.PipeWriter,
	cause error,
) {
	if closeErr := writer.CloseWithError(cause); closeErr != nil && !errors.Is(closeErr, io.ErrClosedPipe) {
		p.logBlobStreamCacheError(ctx, req, "close streamed blob cache writer after scheduler failure failed", closeErr)
	}
	if closeErr := reader.CloseWithError(cause); closeErr != nil && !errors.Is(closeErr, io.ErrClosedPipe) {
		p.logBlobStreamCacheError(ctx, req, "close streamed blob cache reader after scheduler failure failed", closeErr)
	}
}

func (p blobProxy) submitStreamedBlobCache(task func()) error {
	if task == nil {
		return nil
	}
	if p.streamScheduler == nil {
		return errBlobStreamSchedulerUnavailable
	}
	if capacity, ok := p.streamScheduler.(schedulerCapacity); ok && capacity.Free() == 0 {
		return errBlobStreamSchedulerSaturated
	}
	if err := p.streamScheduler.Submit(task); err != nil {
		return wrapError(err, "submit streamed blob cache task")
	}
	return nil
}

type streamedBlob struct {
	reader    *io.PipeReader
	digest    string
	size      int64
	headers   http.Header
	mediaType string
}

func (p blobProxy) storeStreamedBlob(ctx context.Context, req BlobRequest, blob streamedBlob) error {
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
		return err
	}
	if err := p.upsertBlobRecords(ctx, req, stored.info, blob.mediaType); err != nil {
		p.logBlobStreamCacheError(ctx, req, "record streamed blob cache metadata failed", err)
		return err
	}
	p.publishCacheStore(ctx, req, stored.info.Size, stored.info.Digest)
	return nil
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
	cacheDone   <-chan error
	onCacheFail func(error)
	completed   bool
}

func newBlobTeeReadCloser(source io.ReadCloser, cache *io.PipeWriter, cacheDone <-chan error, onCacheFail func(error)) io.ReadCloser {
	return &blobTeeReadCloser{
		source:      source,
		cache:       cache,
		cacheDone:   cacheDone,
		onCacheFail: onCacheFail,
	}
}

func (r *blobTeeReadCloser) Read(buffer []byte) (int, error) {
	n, readErr := r.source.Read(buffer)
	if n > 0 {
		r.writeCache(buffer[:n])
	}
	if readErr != nil {
		r.completed = errors.Is(readErr, io.EOF)
		r.closeCache(readErr)
	}
	return n, blobSourceReadError(readErr)
}

func (r *blobTeeReadCloser) Close() error {
	r.closeCache(io.ErrClosedPipe)
	var err error
	if closeErr := r.source.Close(); closeErr != nil {
		err = wrapError(closeErr, "close streamed blob source")
	}
	if r.completed {
		err = errors.Join(err, r.waitCache())
	}
	return err
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

func (r *blobTeeReadCloser) waitCache() error {
	if r.cacheDone == nil {
		return nil
	}
	if err := <-r.cacheDone; err != nil {
		return wrapError(err, "commit streamed blob cache")
	}
	return nil
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
