//go:build release

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type releaseConfig struct {
	RepoRoot        string
	DistDir         string
	VersionTag      string
	Version         string
	Commit          string
	RegistryImage   string
	DockerHubImage  string
	GoReleaserImage string
	SourceURL       string
	Parallelism     int
	Clean           bool
	DryRun          bool
	SkipGitHub      bool
	SkipDocker      bool
}

func parseConfig(ctx context.Context, args []string) (releaseConfig, error) {
	cfg := defaultReleaseConfig()
	fs := flag.NewFlagSet("regimux-release", flag.ContinueOnError)
	fs.StringVar(&cfg.RepoRoot, "repo", cfg.RepoRoot, "repository root")
	fs.StringVar(&cfg.DistDir, "dist", cfg.DistDir, "GoReleaser dist directory")
	fs.StringVar(&cfg.VersionTag, "version", cfg.VersionTag, "release version or tag, for example v1.2.7")
	fs.StringVar(&cfg.RegistryImage, "registry-image", cfg.RegistryImage, "GHCR image name")
	fs.StringVar(&cfg.DockerHubImage, "dockerhub-image", cfg.DockerHubImage, "Docker Hub image name")
	fs.StringVar(&cfg.GoReleaserImage, "goreleaser-image", cfg.GoReleaserImage, "GoReleaser container image")
	fs.StringVar(&cfg.SourceURL, "source-url", cfg.SourceURL, "OCI image source label")
	fs.IntVar(&cfg.Parallelism, "parallelism", cfg.Parallelism, "GoReleaser parallelism")
	fs.BoolVar(&cfg.Clean, "clean", cfg.Clean, "pass --clean to GoReleaser")
	fs.BoolVar(&cfg.DryRun, "dry-run", cfg.DryRun, "print commands without executing them")
	fs.BoolVar(&cfg.SkipGitHub, "skip-github", cfg.SkipGitHub, "skip GoReleaser GitHub release/assets")
	fs.BoolVar(&cfg.SkipDocker, "skip-docker", cfg.SkipDocker, "skip Docker image build and push")
	if err := fs.Parse(args); err != nil {
		return releaseConfig{}, err
	}
	return finalizeReleaseConfig(ctx, cfg)
}

func defaultReleaseConfig() releaseConfig {
	return releaseConfig{
		RepoRoot:        ".",
		DistDir:         "dist",
		RegistryImage:   envDefault("REGISTRY_IMAGE", "ghcr.io/lyonbrown4d/regimux"),
		DockerHubImage:  envDefault("DOCKERHUB_IMAGE", "lyonbrown4d/regimux"),
		GoReleaserImage: envDefault("GORELEASER_IMAGE", "goreleaser/goreleaser:v2.12.2"),
		SourceURL:       envDefault("REGIMUX_SOURCE_URL", "https://github.com/lyonbrown4d/regimux"),
		Parallelism:     1,
		Clean:           true,
	}
}

func finalizeReleaseConfig(ctx context.Context, cfg releaseConfig) (releaseConfig, error) {
	repoRoot, err := filepath.Abs(cfg.RepoRoot)
	if err != nil {
		return releaseConfig{}, fmt.Errorf("resolve repository root: %w", err)
	}
	cfg.RepoRoot = repoRoot
	cfg.DistDir, err = resolvePath(cfg.RepoRoot, cfg.DistDir)
	if err != nil {
		return releaseConfig{}, err
	}
	if cfg.Parallelism <= 0 {
		return releaseConfig{}, errors.New("parallelism must be positive")
	}
	return finalizeVersion(ctx, cfg)
}

func finalizeVersion(ctx context.Context, cfg releaseConfig) (releaseConfig, error) {
	if strings.TrimSpace(cfg.VersionTag) == "" {
		tag, err := gitOutput(ctx, cfg.RepoRoot, "describe", "--tags", "--exact-match")
		if err != nil {
			return releaseConfig{}, fmt.Errorf("detect exact release tag: %w", err)
		}
		cfg.VersionTag = tag
	}
	cfg.VersionTag, cfg.Version = normalizeVersion(cfg.VersionTag)
	commit, err := gitOutput(ctx, cfg.RepoRoot, "rev-parse", "HEAD")
	if err != nil {
		return releaseConfig{}, fmt.Errorf("detect release commit: %w", err)
	}
	cfg.Commit = commit
	return cfg, nil
}

func normalizeVersion(value string) (string, string) {
	version := strings.TrimSpace(value)
	version = strings.TrimPrefix(version, "refs/tags/")
	if bare, ok := strings.CutPrefix(version, "v"); ok {
		return version, bare
	}
	return "v" + version, version
}

func resolvePath(repoRoot, value string) (string, error) {
	if !filepath.IsAbs(value) {
		value = filepath.Join(repoRoot, value)
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", value, err)
	}
	return abs, nil
}

func envDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
