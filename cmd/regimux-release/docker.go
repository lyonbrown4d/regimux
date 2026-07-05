//go:build release

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func publishDockerImages(ctx context.Context, cfg releaseConfig) error {
	contextDir := filepath.Join(cfg.DistDir, "docker-context")
	if cfg.DryRun {
		if err := writeStdoutf("+ prepare docker context %s\n", contextDir); err != nil {
			return err
		}
	} else if err := prepareDockerContext(cfg, contextDir); err != nil {
		return err
	}
	if err := buildDockerVariant(ctx, cfg, "alpine", contextDir, alpineTags(cfg)); err != nil {
		return err
	}
	return buildDockerVariant(ctx, cfg, "debian", contextDir, debianTags(cfg))
}

func prepareDockerContext(cfg releaseConfig, contextDir string) error {
	if err := requireInside(cfg.RepoRoot, contextDir); err != nil {
		return err
	}
	if err := os.RemoveAll(contextDir); err != nil {
		return fmt.Errorf("remove docker context: %w", err)
	}
	for _, dir := range []string{"linux/amd64", "linux/arm64", "configs"} {
		if err := os.MkdirAll(filepath.Join(contextDir, dir), 0o750); err != nil {
			return fmt.Errorf("create docker context directory %s: %w", dir, err)
		}
	}
	copies := []fileCopy{
		{filepath.Join(cfg.DistDir, "regimuxd-linux_linux_amd64_v1", "regimuxd"), filepath.Join(contextDir, "linux", "amd64", "regimuxd")},
		{filepath.Join(cfg.DistDir, "regimuxd-linux_linux_arm64_v8.0", "regimuxd"), filepath.Join(contextDir, "linux", "arm64", "regimuxd")},
		{filepath.Join(cfg.RepoRoot, "configs", "regimux.minimal.hcl"), filepath.Join(contextDir, "configs", "regimux.minimal.hcl")},
	}
	for _, item := range copies {
		if err := copyFile(item.src, item.dst); err != nil {
			return err
		}
	}
	return nil
}

func buildDockerVariant(ctx context.Context, cfg releaseConfig, variant, contextDir string, tags []string) error {
	args := commonBuildArgs(cfg)
	args = append(args, "-f", filepath.Join(cfg.RepoRoot, "docker", variant+".Dockerfile"))
	for _, tag := range tags {
		args = append(args, "-t", tag)
	}
	args = append(args, contextDir)
	if err := runDocker(ctx, args, nil, cfg.DryRun); err != nil {
		return fmt.Errorf("build and push %s docker image: %w", variant, err)
	}
	return nil
}

func commonBuildArgs(cfg releaseConfig) []string {
	return []string{
		"buildx", "build",
		"--platform", "linux/amd64,linux/arm64",
		"--push",
		"--label", "org.opencontainers.image.title=regimux",
		"--label", "org.opencontainers.image.description=Read-only OCI and Docker Registry V2 multi-upstream proxy mirror gateway",
		"--label", "org.opencontainers.image.source=" + cfg.SourceURL,
		"--label", "org.opencontainers.image.revision=" + cfg.Commit,
		"--label", "org.opencontainers.image.version=" + cfg.Version,
	}
}

func alpineTags(cfg releaseConfig) []string {
	return imageTags(cfg, cfg.VersionTag, cfg.VersionTag+"-alpine", cfg.Version, cfg.Version+"-alpine", "latest", "alpine")
}

func debianTags(cfg releaseConfig) []string {
	return imageTags(cfg, cfg.VersionTag+"-debian", cfg.Version+"-debian", "latest-debian", "debian")
}

func imageTags(cfg releaseConfig, tags ...string) []string {
	images := []string{cfg.RegistryImage, cfg.DockerHubImage}
	out := make([]string, 0, len(images)*len(tags))
	for _, image := range images {
		if image == "" {
			continue
		}
		for _, tag := range tags {
			out = append(out, image+":"+tag)
		}
	}
	return out
}

type fileCopy struct {
	src string
	dst string
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return closeAfterError(in, fmt.Errorf("create %s: %w", dst, err))
	}
	copyErr := copyOpenFile(in, out, src, dst)
	return copyErr
}

func copyOpenFile(in *os.File, out *os.File, src, dst string) error {
	_, copyErr := io.Copy(out, in)
	closeOutErr := out.Close()
	closeInErr := in.Close()
	if copyErr != nil || closeOutErr != nil || closeInErr != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, errors.Join(copyErr, closeOutErr, closeInErr))
	}
	return nil
}

func closeAfterError(file *os.File, err error) error {
	if closeErr := file.Close(); closeErr != nil {
		return errors.Join(err, fmt.Errorf("close source after error: %w", closeErr))
	}
	return err
}

func requireInside(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return fmt.Errorf("resolve relative path: %w", err)
	}
	if rel == "." || rel == "" || rel == ".." || filepath.IsAbs(rel) || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return fmt.Errorf("refusing to operate outside repository: %s", target)
	}
	return nil
}
