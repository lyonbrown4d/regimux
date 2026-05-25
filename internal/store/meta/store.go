package meta

import (
	"context"
	"time"
)

type Store interface {
	UpstreamByAlias(ctx context.Context, alias string) (*Upstream, error)
	RepositoryByName(ctx context.Context, upstreamID int64, name string) (*Repository, error)
}

type Upstream struct {
	ID              int64
	Alias           string
	RegistryURL     string
	DefaultNamespace string
	AuthType        string
	Enabled         bool
	TagTTL          time.Duration
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Repository struct {
	ID          int64
	UpstreamID  int64
	Name        string
	CreatedAt   time.Time
	LastPullAt  *time.Time
}

