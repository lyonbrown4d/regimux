package observability

import (
	"context"
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/logx"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/samber/oops"
)

var Module = dix.NewModule("observability",
	dix.Providers(
		dix.ProviderErr1[*slog.Logger, config.Config](newLogger),
	),
	dix.Hooks(
		dix.OnStop[*slog.Logger](
			closeLogger,
			dix.LifecycleName("regimux.logger_close"),
			dix.LifecyclePriority(-240),
		),
	),
)

func newLogger(cfg config.Config) (*slog.Logger, error) {
	return NewLogger(cfg.Log)
}

func closeLogger(_ context.Context, logger *slog.Logger) error {
	if logger == nil {
		return nil
	}
	if err := logx.Close(logger); err != nil {
		return oops.Wrapf(err, "close logger")
	}
	return nil
}
