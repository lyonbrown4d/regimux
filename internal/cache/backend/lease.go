package backend

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrLeaseLost = errors.New("cache backend lease is no longer held")

type Lease interface {
	Release(ctx context.Context) error
	Extend(ctx context.Context, ttl time.Duration) error
}

type LeaseBackend interface {
	AcquireLease(ctx context.Context, key string, ttl time.Duration) (Lease, bool, error)
}

type lockClient interface {
	Acquire(ctx context.Context, key string, token string, ttl time.Duration) (bool, error)
	Release(ctx context.Context, key string, token string) (bool, error)
	Extend(ctx context.Context, key string, token string, ttl time.Duration) (bool, error)
}

type kvLease struct {
	client lockClient
	key    string
	token  string
}

func (b *KV) AcquireLease(ctx context.Context, key string, ttl time.Duration) (Lease, bool, error) {
	if b == nil || b.client == nil {
		return nil, true, nil
	}
	client, ok := b.client.(lockClient)
	if !ok {
		return nil, true, nil
	}
	key, err := b.key(key)
	if err != nil {
		return nil, false, err
	}
	token := uuid.NewString()
	acquired, err := client.Acquire(ctx, key, token, normalizeRemoteTTL(ttl))
	if err != nil {
		return nil, false, wrapError(err, "acquire kv cache lease")
	}
	if !acquired {
		return nil, false, nil
	}
	return &kvLease{client: client, key: key, token: token}, true, nil
}

func (l *kvLease) Release(ctx context.Context) error {
	if l == nil || l.client == nil {
		return nil
	}
	released, err := l.client.Release(ctx, l.key, l.token)
	if err != nil {
		return wrapError(err, "release kv cache lease")
	}
	if !released {
		return ErrLeaseLost
	}
	return nil
}

func (l *kvLease) Extend(ctx context.Context, ttl time.Duration) error {
	if l == nil || l.client == nil {
		return nil
	}
	extended, err := l.client.Extend(ctx, l.key, l.token, normalizeRemoteTTL(ttl))
	if err != nil {
		return wrapError(err, "extend kv cache lease")
	}
	if !extended {
		return ErrLeaseLost
	}
	return nil
}

var _ LeaseBackend = (*KV)(nil)
