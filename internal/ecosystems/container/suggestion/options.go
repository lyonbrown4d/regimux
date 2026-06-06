package suggestion

import (
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
)

const (
	defaultTagTTL          = 30 * time.Minute
	defaultRepositoryTTL   = 6 * time.Hour
	defaultNegativeTTL     = 5 * time.Minute
	defaultRefreshTimeout  = 2 * time.Second
	defaultTagPageSize     = 1000
	defaultMaxTagPages     = 3
	defaultRepositoryLimit = 500
	defaultMaxSuggestions  = 3
)

type Options struct {
	TagTTL              time.Duration
	RepositoryTTL       time.Duration
	NegativeTTL         time.Duration
	RefreshTimeout      time.Duration
	TagPageSize         int
	MaxTagPages         int
	RepositoryLimit     int
	MaxSuggestions      int
	DisableAsyncRefresh bool
}

func OptionsFromConfig(cfg config.Config) Options {
	opts := Options{
		TagTTL:          cfg.Cache.Tags.TTL,
		RepositoryTTL:   defaultRepositoryTTL,
		NegativeTTL:     defaultNegativeTTL,
		RefreshTimeout:  defaultRefreshTimeout,
		TagPageSize:     cfg.Cache.Tags.MaxPageSize,
		MaxTagPages:     defaultMaxTagPages,
		RepositoryLimit: defaultRepositoryLimit,
		MaxSuggestions:  defaultMaxSuggestions,
	}
	return normalizeOptions(opts)
}

func normalizeOptions(opts Options) Options {
	if opts.TagTTL <= 0 {
		opts.TagTTL = defaultTagTTL
	}
	if opts.RepositoryTTL <= 0 {
		opts.RepositoryTTL = defaultRepositoryTTL
	}
	if opts.NegativeTTL <= 0 {
		opts.NegativeTTL = defaultNegativeTTL
	}
	if opts.RefreshTimeout <= 0 {
		opts.RefreshTimeout = defaultRefreshTimeout
	}
	if opts.TagPageSize <= 0 {
		opts.TagPageSize = defaultTagPageSize
	}
	if opts.MaxTagPages <= 0 {
		opts.MaxTagPages = defaultMaxTagPages
	}
	if opts.RepositoryLimit <= 0 {
		opts.RepositoryLimit = defaultRepositoryLimit
	}
	if opts.MaxSuggestions <= 0 {
		opts.MaxSuggestions = defaultMaxSuggestions
	}
	return opts
}
