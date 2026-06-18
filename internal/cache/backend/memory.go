package backend

import (
	"context"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

type MemoryOptions struct {
	MaxItems int
	Prefix   string
}

type Memory struct {
	cache  *lru.Cache[string, memoryEntry]
	prefix string
}

var _ Backend = (*Memory)(nil)

func NewMemory(opts MemoryOptions) *Memory {
	cache, err := lru.New[string, memoryEntry](memoryMaxItems(opts.MaxItems))
	if err != nil {
		panic(err)
	}
	return &Memory{
		cache:  cache,
		prefix: normalizePrefix(opts.Prefix),
	}
}

func (m *Memory) Get(_ context.Context, key string) ([]byte, bool, error) {
	key, err := m.key(key)
	if err != nil {
		return nil, false, err
	}
	entry, ok := m.cache.Get(key)
	if !ok {
		return nil, false, nil
	}
	if entry.expired(time.Now()) {
		m.cache.Remove(key)
		return nil, false, nil
	}
	return cloneBytes(entry.value), true, nil
}

func (m *Memory) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	key, err := m.key(key)
	if err != nil {
		return err
	}
	m.cache.Add(key, newMemoryEntry(value, ttl))
	return nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	key, err := m.key(key)
	if err != nil {
		return err
	}
	m.cache.Remove(key)
	return nil
}

func (m *Memory) Close() error {
	m.cache.Purge()
	return nil
}

func (m *Memory) key(key string) (string, error) {
	return cacheKey(m.prefix, key)
}

type memoryEntry struct {
	value     []byte
	expiresAt time.Time
}

func newMemoryEntry(value []byte, ttl time.Duration) memoryEntry {
	entry := memoryEntry{value: cloneBytes(value)}
	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	}
	return entry
}

func (e memoryEntry) expired(now time.Time) bool {
	return !e.expiresAt.IsZero() && !now.Before(e.expiresAt)
}

func memoryMaxItems(maxItems int) int {
	if maxItems > 0 {
		return maxItems
	}
	return int(^uint(0) >> 1)
}
