package backend

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/simplelru"
)

type MemoryOptions struct {
	MaxItems int
	MaxBytes int64
	Prefix   string
}

const (
	defaultMemoryMaxItems = 10_000
	defaultMemoryMaxBytes = 64 << 20
)

type Memory struct {
	mu           sync.Mutex
	cache        *simplelru.LRU[string, memoryEntry]
	maxBytes     int64
	currentBytes int64
	prefix       string
}

var _ Backend = (*Memory)(nil)
var _ PrefixDeleter = (*Memory)(nil)

func NewMemory(opts MemoryOptions) *Memory {
	memory := &Memory{
		maxBytes: memoryMaxBytes(opts.MaxBytes),
		prefix:   normalizePrefix(opts.Prefix),
	}
	cache, err := simplelru.NewLRU[string, memoryEntry](memoryMaxItems(opts.MaxItems), func(_ string, entry memoryEntry) {
		memory.currentBytes -= entry.weight
	})
	if err != nil {
		panic(err)
	}
	memory.cache = cache
	return memory
}

func (m *Memory) Get(_ context.Context, key string) ([]byte, bool, error) {
	key, err := m.key(key)
	if err != nil {
		return nil, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
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
	weight := int64(len(key)) + int64(len(value))
	m.mu.Lock()
	defer m.mu.Unlock()
	// Replacing with an uncacheable value must also remove the old entry.
	m.cache.Remove(key)
	if m.maxBytes > 0 && weight > m.maxBytes {
		return nil
	}
	for m.maxBytes > 0 && m.currentBytes+weight > m.maxBytes {
		if _, _, ok := m.cache.RemoveOldest(); !ok {
			break
		}
	}
	entry := newMemoryEntry(value, ttl, weight)
	m.currentBytes += weight
	m.cache.Add(key, entry)
	return nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	key, err := m.key(key)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache.Remove(key)
	return nil
}

func (m *Memory) DeletePrefix(ctx context.Context, prefix string) error {
	if err := validateDeletePrefix(prefix); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return wrapError(err, "delete memory cache prefix")
	}
	physicalPrefix, err := m.key(prefix)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range m.cache.Keys() {
		if err := ctx.Err(); err != nil {
			return wrapError(err, "delete memory cache prefix")
		}
		if strings.HasPrefix(key, physicalPrefix) {
			m.cache.Remove(key)
		}
	}
	return nil
}

func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache.Purge()
	return nil
}

func (m *Memory) key(key string) (string, error) {
	return cacheKey(m.prefix, key)
}

type memoryEntry struct {
	value     []byte
	expiresAt time.Time
	weight    int64
}

func newMemoryEntry(value []byte, ttl time.Duration, weight int64) memoryEntry {
	entry := memoryEntry{value: cloneBytes(value), weight: weight}
	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	}
	return entry
}

func memoryMaxBytes(maxBytes int64) int64 {
	if maxBytes > 0 {
		return maxBytes
	}
	return defaultMemoryMaxBytes
}

func (e memoryEntry) expired(now time.Time) bool {
	return !e.expiresAt.IsZero() && !now.Before(e.expiresAt)
}

func memoryMaxItems(maxItems int) int {
	if maxItems > 0 {
		return maxItems
	}
	return defaultMemoryMaxItems
}
