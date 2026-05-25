package backend

import (
	"context"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

type MemoryOptions struct {
	MaxItems int
	Prefix   string
}

type Memory struct {
	cache  *ttlcache.Cache[string, []byte]
	prefix string
}

var _ Backend = (*Memory)(nil)

func NewMemory(opts MemoryOptions) *Memory {
	cacheOpts := make([]ttlcache.Option[string, []byte], 0, 1)
	if opts.MaxItems > 0 {
		cacheOpts = append(cacheOpts, ttlcache.WithCapacity[string, []byte](uint64(opts.MaxItems)))
	}

	return &Memory{
		cache:  ttlcache.New[string, []byte](cacheOpts...),
		prefix: normalizePrefix(opts.Prefix),
	}
}

func (m *Memory) Get(_ context.Context, key string) ([]byte, bool, error) {
	key, err := m.key(key)
	if err != nil {
		return nil, false, err
	}
	item := m.cache.Get(key, ttlcache.WithDisableTouchOnHit[string, []byte]())
	if item == nil {
		return nil, false, nil
	}
	return cloneBytes(item.Value()), true, nil
}

func (m *Memory) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	key, err := m.key(key)
	if err != nil {
		return err
	}
	m.cache.Set(key, cloneBytes(value), normalizeTTL(ttl))
	return nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	key, err := m.key(key)
	if err != nil {
		return err
	}
	m.cache.Delete(key)
	return nil
}

func (m *Memory) Close() error {
	m.cache.DeleteAll()
	return nil
}

func (m *Memory) key(key string) (string, error) {
	return cacheKey(m.prefix, key)
}

func normalizeTTL(ttl time.Duration) time.Duration {
	if ttl < 0 {
		return ttlcache.NoTTL
	}
	return ttl
}
