package observability

import (
	"fmt"
	"log/slog"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/logx"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func NewLogger(cfg config.LogConfig) (*slog.Logger, error) {
	opts := collectionlist.NewList[logx.Option]()

	if cfg.Level != "" {
		level, err := logx.ParseLevel(cfg.Level)
		if err != nil {
			return nil, fmt.Errorf("parse log level %q: %w", cfg.Level, err)
		}
		opts.Add(logx.WithLevel(level))
	}
	opts.Add(
		logx.WithConsole(cfg.Console),
		logx.WithCaller(cfg.AddCaller),
		logx.WithLocalTime(cfg.LocalTime),
		logx.WithCompress(cfg.Compress),
	)
	if cfg.NoColor {
		opts.Add(logx.WithNoColor())
	}
	if cfg.File != "" {
		opts.Add(logx.WithFile(cfg.File))
	}
	if cfg.MaxSizeMB > 0 || cfg.MaxAgeDays > 0 || cfg.MaxBackups > 0 {
		opts.Add(logx.WithFileRotation(cfg.MaxSizeMB, cfg.MaxAgeDays, cfg.MaxBackups))
	}
	if cfg.TimeFormat != "" {
		opts.Add(logx.WithTimeFormat(cfg.TimeFormat))
	}
	if cfg.SetDefault {
		opts.Add(logx.WithGlobalLogger())
	}

	logger, err := logx.New(opts.Values()...)
	if err != nil {
		return nil, fmt.Errorf("create logx logger: %w", err)
	}
	if cfg.SetDefault {
		logx.SetDefault(logger)
	}
	return logger.With("service", "regimux"), nil
}
