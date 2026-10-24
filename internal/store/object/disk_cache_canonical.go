package object

import (
	"context"
	"fmt"
	"io"
)

func (s *DiskCacheStore) canonicalOpenError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("open canonical object: %w", err)
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errDiskCacheClosed
	}
	return nil
}

func readerCloseError(reader io.ReadCloser) error {
	if reader == nil {
		return nil
	}
	if err := reader.Close(); err != nil {
		return fmt.Errorf("close canonical object reader: %w", err)
	}
	return nil
}
