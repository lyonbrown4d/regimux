package artifactstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// responseBody observes native transport reads without providing WriterTo,
// which could bypass accounting. Close is safe when a transport closes twice.
type responseBody struct {
	ctx      context.Context
	req      ResponseRequest
	read     int64
	reported bool
	once     sync.Once
	closeErr error
}

func (b *responseBody) Read(payload []byte) (int, error) {
	n, err := b.req.Body.Read(payload)
	b.read += int64(n)
	if err != nil && !b.reported {
		b.reported = true
		b.observeRead(err)
	}
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read artifact response: %w", err)
	}
	return n, nil
}

func (b *responseBody) observeRead(err error) {
	req := b.req.copyRequest(nil)
	if !errors.Is(err, io.EOF) {
		logFailure(b.ctx, req, "artifact response stream failed", b.read, err)
		return
	}
	if b.req.Size >= 0 && b.read != b.req.Size {
		logFailure(b.ctx, req, "artifact response body length mismatch", b.read, io.ErrUnexpectedEOF)
	}
}

func (b *responseBody) Close() error {
	b.once.Do(func() {
		b.closeErr = b.req.Body.Close()
		if b.closeErr != nil {
			logFailure(b.ctx, b.req.copyRequest(nil), "close artifact response body failed", 0, b.closeErr)
		}
	})
	if b.closeErr != nil {
		return fmt.Errorf("close artifact response body: %w", b.closeErr)
	}
	return nil
}

func (b *responseBody) close() {
	// Close reports its own failure with the response identity.
	if err := b.Close(); err != nil {
		return
	}
}
