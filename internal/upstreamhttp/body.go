package upstreamhttp

import (
	"errors"
	"fmt"
	"io"
)

var (
	ErrBodyTooLarge = errors.New("upstream response body exceeds limit")
	ErrInvalidLimit = errors.New("upstream response body limit must be positive")
)

func ReadAllLimited(reader io.Reader, limit int64) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("upstream response body is nil")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidLimit, limit)
	}

	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read upstream response body: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%w: %d bytes", ErrBodyTooLarge, limit)
	}
	return body, nil
}
