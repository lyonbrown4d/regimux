package backend

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/arcgolabs/kvx"
	redisadapter "github.com/arcgolabs/kvx/adapter/redis"
	valkeyadapter "github.com/arcgolabs/kvx/adapter/valkey"
	"github.com/lyonbrown4d/regimux/internal/cache/backend/valkeyreply"
	goredis "github.com/redis/go-redis/v9"
	"github.com/valkey-io/valkey-go"
)

type KVClient interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, expiration time.Duration) error
	Delete(ctx context.Context, key string) error
	Close() error
}

type KV struct {
	client KVClient
	prefix string
}

type KVOptions struct {
	Addrs    []string
	Username string
	Password string
	DB       int
	Prefix   string
	Logger   *slog.Logger
	Debug    bool
}

const (
	releaseLockScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
	return redis.call('DEL', KEYS[1])
end
return 0
`

	extendLockScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
	return redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 0
`
)

func NewRedis(opts KVOptions) (*KV, error) {
	if len(opts.Addrs) == 0 {
		return nil, ErrNoAddrs
	}

	client := goredis.NewClient(&goredis.Options{
		Addr:     opts.Addrs[0],
		Username: opts.Username,
		Password: opts.Password,
		DB:       opts.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, joinError("ping redis cache", err, client.Close())
	}

	return NewKV(redisadapter.NewFromClient(client), opts.Prefix), nil
}

func NewValkey(opts KVOptions) (*KV, error) {
	if len(opts.Addrs) == 0 {
		return nil, ErrNoAddrs
	}

	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: opts.Addrs,
		Username:    opts.Username,
		Password:    opts.Password,
		SelectDB:    opts.DB,
	})
	if err != nil {
		return nil, wrapError(err, "create valkey client")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Do(ctx, client.B().Ping().Build()).Error(); err != nil {
		client.Close()
		return nil, wrapError(err, "ping valkey cache")
	}

	return NewKV(&valkeyKVClient{
		cache:  valkeyadapter.NewFromClient(client),
		client: client,
	}, opts.Prefix), nil
}

func NewKV(client KVClient, prefix string) *KV {
	return &KV{
		client: client,
		prefix: strings.Trim(strings.TrimSpace(prefix), ":"),
	}
}

func (b *KV) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if b == nil || b.client == nil {
		return nil, false, nil
	}
	key, err := b.key(key)
	if err != nil {
		return nil, false, err
	}
	value, err := b.client.Get(ctx, key)
	if err != nil {
		if kvx.IsNil(err) {
			return nil, false, nil
		}
		return nil, false, wrapError(err, "get kv cache entry")
	}
	return cloneBytes(value), true, nil
}

func (b *KV) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if b == nil || b.client == nil {
		return nil
	}
	key, err := b.key(key)
	if err != nil {
		return err
	}
	if err := b.client.Set(ctx, key, cloneBytes(value), normalizeRemoteTTL(ttl)); err != nil {
		return wrapError(err, "set kv cache entry")
	}
	return nil
}

func (b *KV) Delete(ctx context.Context, key string) error {
	if b == nil || b.client == nil {
		return nil
	}
	key, err := b.key(key)
	if err != nil {
		return err
	}
	if err := b.client.Delete(ctx, key); err != nil {
		return wrapError(err, "delete kv cache entry")
	}
	return nil
}

func (b *KV) Close() error {
	if b == nil || b.client == nil {
		return nil
	}
	if err := b.client.Close(); err != nil {
		return wrapError(err, "close kv cache client")
	}
	return nil
}

func (b *KV) key(key string) (string, error) {
	return cacheKey(b.prefix, strings.TrimLeft(strings.TrimSpace(key), ":"))
}

type valkeyKVClient struct {
	client valkey.Client
	cache  *valkeyadapter.Adapter
}

func (c *valkeyKVClient) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := c.cache.Get(ctx, key)
	if err != nil {
		return nil, wrapError(err, "get valkey cache value")
	}
	return value, nil
}

func (c *valkeyKVClient) Set(ctx context.Context, key string, value []byte, expiration time.Duration) error {
	if err := c.cache.Set(ctx, key, value, expiration); err != nil {
		return wrapError(err, "set valkey cache value")
	}
	return nil
}

func (c *valkeyKVClient) Delete(ctx context.Context, key string) error {
	if err := c.cache.Delete(ctx, key); err != nil {
		return wrapError(err, "delete valkey cache value")
	}
	return nil
}

func (c *valkeyKVClient) Close() error {
	if err := c.cache.Close(); err != nil {
		return wrapError(err, "close valkey cache")
	}
	return nil
}

func (c *valkeyKVClient) Acquire(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	resp := c.client.Do(ctx, c.client.B().Set().Key(key).Value(token).Nx().Px(ttl).Build())
	if err := resp.Error(); err != nil {
		if valkey.IsValkeyNil(err) {
			return false, nil
		}
		return false, wrapError(err, "acquire valkey cache lease")
	}

	return true, nil
}

func (c *valkeyKVClient) Release(ctx context.Context, key, token string) (bool, error) {
	command := c.client.B().Eval().
		Script(releaseLockScript).
		Numkeys(1).
		Key(key).
		Arg(token).
		Build()
	resp := c.client.Do(ctx, command)
	released, err := valkeyreply.ParseLock(resp)
	if err != nil {
		return false, wrapError(err, "release valkey cache lease")
	}
	return released, nil
}

func (c *valkeyKVClient) Extend(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	command := c.client.B().Eval().
		Script(extendLockScript).
		Numkeys(1).
		Key(key).
		Arg(token).
		Arg(strconv.FormatInt(ttl.Milliseconds(), 10)).
		Build()
	resp := c.client.Do(ctx, command)
	extended, err := valkeyreply.ParseLock(resp)
	if err != nil {
		return false, wrapError(err, "extend valkey cache lease")
	}
	return extended, nil
}

var _ Backend = (*KV)(nil)
