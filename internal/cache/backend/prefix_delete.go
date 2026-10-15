package backend

import (
	"context"
	"strings"
)

const prefixDeleteBatchSize = 256

type prefixKVClient interface {
	DeletePrefix(ctx context.Context, prefix string) error
}

type keyScanner func(context.Context, uint64, string, int64) ([]string, uint64, error)
type keyBatchDeleter func(context.Context, []string) error

func deletePrefixAcrossNodes(ctx context.Context, prefix string, scanners []keyScanner, remove keyBatchDeleter) error {
	if len(scanners) == 0 {
		return ErrNoCacheNodes
	}
	for _, scan := range scanners {
		if err := deletePrefixByScan(ctx, prefix, scan, remove); err != nil {
			return wrapError(err, "delete cache prefix across nodes")
		}
	}
	return nil
}

func deletePrefixByScan(ctx context.Context, prefix string, scan keyScanner, remove keyBatchDeleter) error {
	if err := validateDeletePrefix(prefix); err != nil {
		return err
	}
	pattern := escapeRedisGlob(prefix) + "*"
	var cursor uint64
	for {
		nextCursor, done, err := deletePrefixScanPage(ctx, cursor, prefix, pattern, scan, remove)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		cursor = nextCursor
	}
}

func deletePrefixScanPage(ctx context.Context, cursor uint64, prefix, pattern string, scan keyScanner, remove keyBatchDeleter) (uint64, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, wrapError(err, "scan cache prefix")
	}
	keys, nextCursor, err := scan(ctx, cursor, pattern, prefixDeleteBatchSize)
	if err != nil {
		return 0, false, wrapError(err, "scan cache prefix")
	}
	matches := make([]string, 0, len(keys))
	for _, key := range keys {
		if strings.HasPrefix(key, prefix) {
			matches = append(matches, key)
		}
	}
	if err := deleteKeyBatches(ctx, matches, remove); err != nil {
		return 0, false, wrapError(err, "delete scanned cache prefix keys")
	}
	return nextCursor, nextCursor == 0, nil
}

func deleteKeyBatches(ctx context.Context, keys []string, remove keyBatchDeleter) error {
	for len(keys) > 0 {
		if err := ctx.Err(); err != nil {
			return wrapError(err, "delete cache prefix batch")
		}
		batchSize := min(len(keys), prefixDeleteBatchSize)
		if err := remove(ctx, keys[:batchSize]); err != nil {
			return err
		}
		keys = keys[batchSize:]
	}
	return nil
}

func escapeRedisGlob(value string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`*`, `\*`,
		`?`, `\?`,
		`[`, `\[`,
		`]`, `\]`,
	).Replace(value)
}
