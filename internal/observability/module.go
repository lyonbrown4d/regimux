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
		dix.Provider1[config.LogConfig, config.Config](func(cfg config.Config) config.LogConfig {
			return cfg.Log
		}),
		dix.ProviderErr1[*slog.Logger, config.LogConfig](NewLogger),
	),
	dix.Hooks(
		dix.OnStop[*slog.Logger](
			closeLogger,
			dix.LifecycleName("regimux.logger_close"),
			dix.LifecyclePriority(-240),
		),
	),
)

func closeLogger(_ context.Context, logger *slog.Logger) error {
	if logger == nil {
		return nil
	}
	if err := logx.Close(logger); err != nil {
		return oops.Wrapf(err, "close logger")
	}
	return nil
}
