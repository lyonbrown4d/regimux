// Package depprefetch warms recently accessed artifacts for dependency ecosystems.
package depprefetch

import (
	"context"
	"log/slog"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

const (
	statusSuccess = "success"
	statusFailed  = "failed"
	statusSkipped = "skipped"

	runStatusCompleted = "completed"
	runStatusFailed    = "failed"

	defaultMaxRecords   = 200
	defaultMinPullCount = 1
)

type FetchFunc func(context.Context, Candidate) (FetchResult, error)

type MetadataStore interface {
	meta.PullRepository
	meta.PrefetchRepository
}

type Dependencies struct {
	Ecosystem string
	Metadata  MetadataStore
	Workers   *worker.Pools
	Logger    *slog.Logger
	Fetch     FetchFunc
}

type Service struct {
	ecosystem string
	metadata  MetadataStore
	workers   *worker.Pools
	logger    *slog.Logger
	fetch     FetchFunc
}

type Candidate struct {
	ScopedAlias string
	Alias       string
	Repository  string
	Reference   string
	Count       int64
	Score       int
}

type FetchResult struct {
	BytesWarmed int64
}
