// Package artifactstream copies cached artifact bodies with structured failure logging.
package artifactstream

import (
	"context"
	"errors"
	"io"
	"log/slog"
)

type CopyRequest struct {
	Destination   io.Writer
	Source        io.Reader
	Logger        *slog.Logger
	Ecosystem     string
	Alias         string
	Reference     string
	Cache         string
	ExpectedBytes int64
}

func Copy(ctx context.Context, req CopyRequest) int64 {

	if req.Destination == nil || req.Source == nil {
		logFailure(ctx, req, "artifact response stream failed", 0, errors.New("stream source and destination are required"))
		return 0
	}

	written, err := io.Copy(req.Destination, req.Source)
	if err != nil {
		logFailure(ctx, req, "artifact response stream failed", written, err)
		return written
	}
	if req.ExpectedBytes >= 0 && written != req.ExpectedBytes {
		logFailure(
			ctx,
			req,
			"artifact response body length mismatch",
			written,
			io.ErrUnexpectedEOF,
		)
	}
	return written
}

func logFailure(
	ctx context.Context,
	req CopyRequest,
	message string,
	written int64,
	err error,
) {
	if req.Logger == nil {
		return
	}
	req.Logger.ErrorContext(
		ctx,
		message,
		"ecosystem", req.Ecosystem,
		"alias", req.Alias,
		"reference", req.Reference,
		"cache", req.Cache,
		"bytes_written", written,
		"expected_bytes", req.ExpectedBytes,
		"error", err,
	)
}
