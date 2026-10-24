package object

import (
	"context"
	"fmt"
	"os"
)

func closeCachedFile(ctx context.Context, store *DiskCacheStore, file *os.File, operation string) error {
	err := file.Close()
	if err != nil {
		wrapped := fmt.Errorf("close cached object during %s: %w", operation, err)
		logCachedFileCloseFailure(ctx, store, operation, wrapped)
		return wrapped
	}
	return nil
}

func discardCachedFileClose(ctx context.Context, store *DiskCacheStore, file *os.File, operation string) {
	if err := file.Close(); err != nil {
		logCachedFileCloseFailure(ctx, store, operation, fmt.Errorf("close cached object during %s: %w", operation, err))
	}
}

func logCachedFileCloseFailure(ctx context.Context, store *DiskCacheStore, operation string, err error) {
	store.logger().WarnContext(ctx, "could not close cached object", "operation", operation, "error", err)
}
