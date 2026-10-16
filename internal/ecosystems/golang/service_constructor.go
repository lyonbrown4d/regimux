package golang

import (
	"log/slog"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type ServiceDependencies struct {
	Config   config.Config
	Cache    *artifactcache.Store
	Metadata artifactcache.MetadataStore
	Objects  object.Store
	Factory  *clientfactory.Factory
	Logger   *slog.Logger
	Events   events.Bus
}

func NewService(deps ServiceDependencies) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
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
		})
	}
	metadata := deps.Metadata
	if metadata == nil {
		metadata = cache.Metadata()
	}
	objects := deps.Objects
	if objects == nil {
		objects = cache.Objects()
	}
	fills := cache.FillTracker()
	if fills == nil {
		fills = artifactcache.NewFillTracker()
	}
	readCache := artifactcache.New(artifactcache.Dependencies{
		Metadata: metadata,
		Objects:  objects,
		Fills:    fills,
		Logger:   logger,
	})
	return &Service{
		cfg:      deps.Config,
		metadata: metadata,
		objects:  objects,
		factory:  factory,
		logger:   logger.With("component", "go"),
		fills:    fills,
		cache:    readCache,
		events:   deps.Events,
	}
}
