package object

import (
	"context"
	"io"
	"time"
)

// S3UploadEvent uses only bounded lifecycle kinds, never object keys or URLs.
type S3UploadEvent struct {
	Kind     string
	Delta    int64
	Duration time.Duration
	Err      error
}

func (s *S3Store) uploadAdmitted(
	ctx context.Context,
	digest, key string,
	reader io.Reader,
	opts PutOptions,
) (result *s3UploadResult, resultErr error) {
	started := time.Now()
	s.emitUpload(ctx, S3UploadEvent{Kind: "waiting", Delta: 1})
	err := s.uploadGate.Acquire(ctx, 1)
	s.emitUpload(ctx, S3UploadEvent{Kind: "waiting", Delta: -1})
	s.emitUpload(ctx, S3UploadEvent{Kind: "admission", Duration: time.Since(started), Err: err})
	if err != nil {
		return nil, wrapS3OperationError("admit upload", digest, err)
	}
	defer s.uploadGate.Release(1)
	s.emitUpload(ctx, S3UploadEvent{Kind: "active", Delta: 1})
	uploadStarted := time.Now()
	defer func() {
		s.emitUpload(ctx, S3UploadEvent{Kind: "active", Delta: -1})
		s.emitUpload(ctx, S3UploadEvent{Kind: "completed", Duration: time.Since(uploadStarted), Err: resultErr})
	}()
	return s.upload(ctx, digest, key, reader, opts)
}

func (s *S3Store) emitUpload(ctx context.Context, event S3UploadEvent) {
	if s.observeUpload != nil {
		s.observeUpload(context.WithoutCancel(ctx), event)
	}
}
