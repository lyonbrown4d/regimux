package npm

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

func acquireMetadataSlot(ctx context.Context, slots chan struct{}) (func(), error) {
	if ctx == nil {
		return nil, errors.New("npm metadata admission context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("check npm metadata admission context: %w", err)
	}
	select {
	case slots <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-slots
			return nil, fmt.Errorf("check npm metadata admission context: %w", err)
		}
		var once sync.Once
		return func() { once.Do(func() { <-slots }) }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for npm metadata admission: %w", ctx.Err())
	}
}
