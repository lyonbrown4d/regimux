package cache

import (
	"log/slog"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type leaseRenewScheduler interface {
	Submit(func()) error
}

type blobStreamScheduler interface {
	Submit(func()) error
}

type schedulerCapacity interface {
	Free() int
}

type ProxyDependencies struct {
	Client              upstream.RegistryClient
	Cache               backend.Backend
	Metadata            meta.Store
	Objects             object.Store
	CacheConfig         config.CacheConfig
	Events              events.Bus
	LeaseScheduler      leaseRenewScheduler
	BlobStreamScheduler blobStreamScheduler
	Logger              *slog.Logger
}
