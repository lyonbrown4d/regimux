package pypi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

const metadataTransformConcurrency = 2

func acquireMetadataSlot(ctx context.Context, slots chan struct{}) (func(), error) {
	if ctx == nil {
		return nil, errors.New("pypi metadata admission context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("check pypi metadata admission context: %w", err)
	}
	select {
	case slots <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-slots
			return nil, fmt.Errorf("check pypi metadata admission context: %w", err)
		}
		var once sync.Once
		return func() { once.Do(func() { <-slots }) }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for pypi metadata admission: %w", ctx.Err())
	}
}

func (s *Service) withSimpleIndexAdmission(ctx context.Context, body io.Reader, consume func([]byte)) bool {
	release, err := acquireMetadataSlot(ctx, s.metadataSlots)
	if err != nil {
		return false
	}
	defer release()

	data, err := upstreamhttp.ReadAllLimited(body, pypiSimpleIndexMaxBytes)
	if err != nil {
		return false
	}
	consume(data)
	return true
}
