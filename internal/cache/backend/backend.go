// Package backend provides byte-oriented cache backends.
package backend

import (
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

var (
	ErrEmptyKey = errors.New("cache backend key is empty")
	ErrNoAddrs  = errors.New("cache backend addrs is empty")
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

func normalizePrefix(prefix string) string {
	return strings.Trim(strings.TrimSpace(prefix), ":")
}

func cloneBytes(in []byte) []byte {
	if in == nil {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func normalizeRemoteTTL(ttl time.Duration) time.Duration {
	if ttl < 0 {
		return 0
	}
	return ttl
}
