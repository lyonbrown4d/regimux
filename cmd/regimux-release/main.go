//go:build release

package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

func main() {
	ctx := context.Background()
	cfg, err := parseConfig(ctx, os.Args[1:])
	if err != nil {
		exitWithError(err, 2)
	}
	if err := run(ctx, cfg); err != nil {
		exitWithError(err, 1)
	}
}

func run(ctx context.Context, cfg releaseConfig) error {
	startedAt := time.Now()
	if !cfg.SkipGitHub {
		if err := runGoReleaser(ctx, cfg); err != nil {
			return err
		}
	}
	if !cfg.SkipDocker {
		if err := publishDockerImages(ctx, cfg); err != nil {
			return err
		}
	}
	if err := writeStdoutf("release %s completed in %s\n", cfg.VersionTag, time.Since(startedAt).Round(time.Second)); err != nil {
		return err
	}
	return nil
}

func exitWithError(err error, code int) {
	if _, printErr := fmt.Fprintln(os.Stderr, err); printErr != nil {
		os.Exit(1)
	}
	os.Exit(code)
}
