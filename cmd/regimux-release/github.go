//go:build release

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func runGoReleaser(ctx context.Context, cfg releaseConfig) error {
	token := ""
	if !cfg.DryRun {
		var err error
		token, err = githubToken(ctx)
		if err != nil {
			return err
		}
	}
	args := []string{
		"run", "--rm",
		"-v", dockerVolume(cfg.RepoRoot, "/work"),
		"-w", "/work",
		"-e", "GITHUB_TOKEN",
		"-e", "GOTOOLCHAIN=auto",
		"-e", "REGISTRY_IMAGE",
		"-e", "DOCKERHUB_IMAGE",
		cfg.GoReleaserImage,
		"release",
		"--parallelism", strconv.Itoa(cfg.Parallelism),
		"--skip=docker",
	}
	if cfg.Clean {
		args = append(args, "--clean")
	}
	env := map[string]string{
		"GITHUB_TOKEN":    token,
		"REGISTRY_IMAGE":  cfg.RegistryImage,
		"DOCKERHUB_IMAGE": cfg.DockerHubImage,
	}
	if err := runDocker(ctx, args, env, cfg.DryRun); err != nil {
		return fmt.Errorf("run goreleaser container: %w", err)
	}
	return nil
}

func githubToken(ctx context.Context) (string, error) {
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		return token, nil
	}
	cmd := exec.CommandContext(ctx, "gh", "auth", "token")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("read GitHub token from gh auth: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", errors.New("GITHUB_TOKEN is empty and gh auth token returned nothing")
	}
	return token, nil
}
