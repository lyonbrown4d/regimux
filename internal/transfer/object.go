package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

// ObjectRequest identifies an immutable object and an optional absolute range.
type ObjectRequest struct {
	Key         string
	Digest      string
	Size        int64
	Options     object.GetOptions
	OpenTimeout time.Duration
	IdleTimeout time.Duration
}

// OpenObject opens and resumes a verified CAS representation within its original
// range. Each request has its own open and idle deadlines; Close cancels transfer.
func OpenObject(ctx context.Context, objects object.Store, request ObjectRequest) (io.ReadCloser, *object.Info, error) {
	if objects == nil {
		return nil, nil, errors.New("artifact object store is not configured")
	}
	request, err := normalizeObjectRequest(request)
	if err != nil {
		return nil, nil, err
	}
	transferCtx, cancel := context.WithCancel(ctx)
	reader, info, err := openObjectAttempt(transferCtx, objects, request)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	start, end := objectResponseBounds(request, info)
	recovered := Recover(transferCtx, reader, info.Size, func(retryCtx context.Context, offset int64) (io.ReadCloser, error) {
		next := request
		next.Options.Range = &object.HTTPRange{Start: start + offset, End: end}
		nextReader, _, openErr := openObjectAttempt(retryCtx, objects, next)
		return nextReader, openErr
	})
	return CancelOnClose(recovered, cancel), info, nil
}

func normalizeObjectRequest(request ObjectRequest) (ObjectRequest, error) {
	if request.Key == "" {
		return ObjectRequest{}, errors.New("artifact object key is required")
	}
	if request.Digest == "" {
		request.Digest = request.Key
	}
	if request.OpenTimeout <= 0 {
		request.OpenTimeout = 2 * time.Second
	}
	if request.IdleTimeout <= 0 {
		request.IdleTimeout = upstreamhttp.DefaultBodyIdleTimeout
	}
	if request.Options.Range == nil {
		return request, nil
	}
	if request.Size <= 0 {
		return ObjectRequest{}, errors.New("artifact range requires the complete object size")
	}
	resolved, err := request.Options.Range.Resolve(request.Size)
	if err != nil {
		return ObjectRequest{}, fmt.Errorf("resolve artifact object range: %w", err)
	}
	request.Options.Range = resolved
	return request, nil
}

func objectResponseBounds(request ObjectRequest, info *object.Info) (int64, int64) {
	if request.Options.Range != nil {
		return request.Options.Range.Start, request.Options.Range.End
	}
	return 0, info.Size - 1
}

type objectAttempt struct {
	ctx    context.Context
	cancel context.CancelFunc
	timer  *time.Timer
}

func openObjectAttempt(ctx context.Context, objects object.Store, request ObjectRequest) (io.ReadCloser, *object.Info, error) {
	attemptCtx, cancel := context.WithCancel(ctx)
	attempt := objectAttempt{ctx: attemptCtx, cancel: cancel, timer: time.AfterFunc(request.OpenTimeout, cancel)}
	reader, info, err := objects.Get(attemptCtx, request.Key, request.Options)
	return attempt.finish(reader, info, request, err)
}

func (attempt objectAttempt) finish(reader io.ReadCloser, info *object.Info, request ObjectRequest, err error) (io.ReadCloser, *object.Info, error) {
	if !attempt.timer.Stop() {
		err = errors.Join(err, context.DeadlineExceeded, ErrObjectOpenTimeout)
	}
	if contextErr := attempt.ctx.Err(); contextErr != nil {
		err = errors.Join(err, contextErr)
	}
	if err == nil {
		err = validateObjectResponse(reader, info, request)
	}
	if err != nil {
		attempt.cancel()
		return nil, nil, closeObjectResponse(reader, err)
	}
	return upstreamhttp.WithIdleTimeout(reader, attempt.cancel, request.IdleTimeout), info, nil
}

func validateObjectResponse(reader io.ReadCloser, info *object.Info, request ObjectRequest) error {
	if reader == nil || info == nil {
		return errors.New("artifact object response is incomplete")
	}
	if info.Digest != request.Digest {
		return fmt.Errorf("artifact object digest %q does not match %q", info.Digest, request.Digest)
	}
	if info.Size < 0 {
		return errors.New("artifact object response has an unknown size")
	}
	expected := request.Size
	if request.Options.Range != nil {
		expected = request.Options.Range.Length()
	}
	if expected < 0 && request.Options.Range == nil {
		expected = info.Size
	}
	if info.Size != expected {
		return fmt.Errorf("artifact object size %d does not match %d", info.Size, expected)
	}
	return nil
}

func closeObjectResponse(reader io.ReadCloser, cause error) error {
	if reader != nil {
		cause = errors.Join(cause, reader.Close())
	}
	return fmt.Errorf("open artifact object: %w", cause)
}
