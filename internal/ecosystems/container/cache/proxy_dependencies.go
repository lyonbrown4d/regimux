package cache

import (
	"context"
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

type manifestClient interface {
	GetManifest(context.Context, upstream.GetManifestRequest) (*upstream.ManifestResponse, error)
}

type blobClient interface {
	GetBlob(context.Context, upstream.GetBlobRequest) (*upstream.BlobResponse, error)
	ConsumeBlob(context.Context, upstream.GetBlobRequest, upstream.BlobConsumeFunc) error
}

type tagClient interface {
	ListTags(context.Context, upstream.ListTagsRequest) (*upstream.TagsResponse, error)
}

type referrerClient interface {
	GetReferrers(context.Context, upstream.ReferrersRequest) (*upstream.ReferrersResponse, error)
	GetManifest(context.Context, upstream.GetManifestRequest) (*upstream.ManifestResponse, error)
}

type registryClient interface {
	manifestClient
	blobClient
	tagClient
	referrerClient
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
