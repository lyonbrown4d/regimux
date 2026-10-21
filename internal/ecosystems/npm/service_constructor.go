package npm

import (
	"log/slog"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
)

func NewService(deps ServiceDependencies) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	factory := deps.Factory
	if factory == nil {
		factory = clientfactory.New(logger)
	}
	cache := deps.Cache
	if cache == nil {
		cache = artifactcache.New(artifactcache.Dependencies{
			Metadata: deps.Metadata,
			Objects:  deps.Objects,
			Logger:   logger,
			Now:      now,
		})
	}
	fills := cache.FillTracker()
	if fills == nil {
		fills = artifactcache.NewFillTracker()
	}
	return &Service{
		cfg:           deps.Config,
		metadata:      deps.Metadata,
		cache:         cache,
		client:        deps.Client,
		factory:       factory,
		logger:        logger.With("component", "npm"),
		publicURL:     strings.TrimRight(deps.Config.Server.PublicURL, "/"),
		metadataTTL:   deps.MetadataTTL,
		fills:         fills,
		metadataSlots: make(chan struct{}, metadataTransformConcurrency),
		now:           now,
		events:        deps.Events,
	}
}
