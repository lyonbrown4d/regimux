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
			return err
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
		if err := ctx.Err(); err != nil {
			return err
		}
		keys, nextCursor, err := scan(ctx, cursor, pattern, prefixDeleteBatchSize)
		if err != nil {
			return err
		}
		matches := make([]string, 0, len(keys))
		for _, key := range keys {
			if strings.HasPrefix(key, prefix) {
				matches = append(matches, key)
			}
		}
		if err := deleteKeyBatches(ctx, matches, remove); err != nil {
			return err
		}
		cursor = nextCursor
		if cursor == 0 {
			return nil
		}
	}
}

func deleteKeyBatches(ctx context.Context, keys []string, remove keyBatchDeleter) error {
	for len(keys) > 0 {
		if err := ctx.Err(); err != nil {
			return err
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
	var escaped strings.Builder
	escaped.Grow(len(value))
	for _, char := range value {
		if strings.ContainsRune(`\*?[]`, char) {
			escaped.WriteByte('\\')
		}
		escaped.WriteRune(char)
	}
	return escaped.String()
}
