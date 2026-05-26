// Package observability builds runtime observability dependencies.
package observability

import (
	"log/slog"

	"github.com/arcgolabs/logx"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/samber/oops"
)

func NewLogger(cfg config.LogConfig) (*slog.Logger, error) {
	opts, err := loggerOptions(cfg)
	if err != nil {
		return nil, err
	}
	logger, err := logx.New(opts...)
	if err != nil {
		return nil, oops.Wrapf(err, "create logx logger")
	}
	if cfg.SetDefault {
		logx.SetDefault(logger)
	}
	return logger.With("service", "regimux"), nil
}

func loggerOptions(cfg config.LogConfig) ([]logx.Option, error) {
	opts := []logx.Option{
		logx.WithConsole(cfg.Console),
		logx.WithCaller(cfg.AddCaller),
		logx.WithLocalTime(cfg.LocalTime),
		logx.WithCompress(cfg.Compress),
	}
	levelOpt, err := levelOption(cfg.Level)
	if err != nil {
		return nil, err
	}
	opts = append(opts, levelOpt...)
	opts = append(opts, fileOptions(cfg)...)
	if cfg.TimeFormat != "" {
		opts = append(opts, logx.WithTimeFormat(cfg.TimeFormat))
	}
	if cfg.SetDefault {
		opts = append(opts, logx.WithGlobalLogger())
	}
	return opts, nil
}

func levelOption(levelName string) ([]logx.Option, error) {
	if levelName == "" {
		return nil, nil
	}
	level, err := logx.ParseLevel(levelName)
	if err != nil {
		return nil, oops.Wrapf(err, "parse log level %q", levelName)
	}
	return []logx.Option{logx.WithLevel(level)}, nil
}

func fileOptions(cfg config.LogConfig) []logx.Option {
	var opts []logx.Option
	if cfg.File != "" {
		opts = append(opts, logx.WithFile(cfg.File))
	}
	if cfg.MaxSizeMB > 0 || cfg.MaxAgeDays > 0 || cfg.MaxBackups > 0 {
		opts = append(opts, logx.WithFileRotation(cfg.MaxSizeMB, cfg.MaxAgeDays, cfg.MaxBackups))
	}
	return opts
}
