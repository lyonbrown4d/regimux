// Package backend provides byte-oriented cache backends.
package backend

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"
)

// Backend is the byte-oriented cache contract used by RegiMux.
type Backend interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Close() error
}

// PrefixDeleter removes cache entries whose logical key begins with a prefix.
// Implementations apply their configured namespace before matching keys.
type PrefixDeleter interface {
	DeletePrefix(ctx context.Context, prefix string) error
}

var (
	ErrEmptyKey                = errors.New("cache backend key is empty")
	ErrNoAddrs                 = errors.New("cache backend addrs is empty")
	ErrNoCacheNodes            = errors.New("cache backend has no nodes to scan")
	ErrPrefixDeleteUnsupported = errors.New("cache backend does not support prefix deletion")
)

func cacheKey(prefix, key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", ErrEmptyKey
	}
	prefix = normalizePrefix(prefix)
	if prefix == "" {
		return key, nil
	}
	return prefix + ":" + key, nil
}

func validateDeletePrefix(prefix string) error {
	if strings.Trim(strings.TrimSpace(prefix), ":") == "" {
		return ErrEmptyKey
	}
	return nil
}

func normalizePrefix(prefix string) string {
	return strings.Trim(strings.TrimSpace(prefix), ":")
}

func cloneBytes(in []byte) []byte {
	return bytes.Clone(in)
}

func normalizeRemoteTTL(ttl time.Duration) time.Duration {
	if ttl < 0 {
		return 0
	}
	return ttl
}
