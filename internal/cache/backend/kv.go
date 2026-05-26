package backend

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/arcgolabs/kvx"
	redisadapter "github.com/arcgolabs/kvx/adapter/redis"
	valkeyadapter "github.com/arcgolabs/kvx/adapter/valkey"
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
		return nil, errors.Join(err, client.Close())
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

	return NewKV(valkeyadapter.NewFromClient(client), opts.Prefix), nil
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

var _ Backend = (*KV)(nil)
