package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/arcgolabs/logx"
	"github.com/lyonbrown4d/regimux/internal/app"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	if err := newRootCommand().Execute(); err != nil {
		slog.Error("regimuxd failed", "error", err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	var configPath string
	var showVersion bool

	cmd := &cobra.Command{
		Use:           "regimuxd",
		Short:         "Run the RegiMux registry proxy mirror gateway",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion {
				fmt.Fprintln(cmd.OutOrStdout(), version)
				return nil
			}
			return run(cmd.Context(), configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "configs/regimux.yaml", "path to regimux config file")
	cmd.Flags().BoolVar(&showVersion, "version", false, "print version and exit")
	return cmd
}

func run(ctx context.Context, configPath string) error {
	cfg, err := config.Load(ctx, configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger, err := observability.NewLogger(cfg.Log)
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}
	defer func() {
		_ = logx.Close(logger)
	}()

	application := app.New(cfg, logger, version)
	return application.RunContext(ctx)
}
