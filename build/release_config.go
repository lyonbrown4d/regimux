package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/goyek/goyek/v3"
)

const (
	defaultRegistryImage   = "ghcr.io/lyonbrown4d/regimux"
	defaultDockerHubImage  = "lyonbrown4d/regimux"
	defaultGoReleaserImage = "goreleaser/goreleaser:v2.12.2"
	defaultSourceURL       = "https://github.com/lyonbrown4d/regimux"
)

var (
	releaseVersion = flag.String(
		"version",
		"",
		"release version; defaults to the exact Git tag at HEAD",
	)
	releaseDistDirectory = flag.String(
		"dist-dir",
		"dist",
		"release artifact directory relative to the repository root",
	)
	releaseRegistryImage = flag.String(
		"registry-image",
		environmentOrDefault("REGISTRY_IMAGE", defaultRegistryImage),
		"GHCR image name",
	)
	releaseDockerHubImage = flag.String(
		"dockerhub-image",
		environmentOrDefault("DOCKERHUB_IMAGE", defaultDockerHubImage),
		"Docker Hub image name",
	)
	releaseGoReleaserImage = flag.String(
		"goreleaser-image",
		environmentOrDefault("GORELEASER_IMAGE", defaultGoReleaserImage),
		"GoReleaser container image",
	)
	releaseSourceURL = flag.String(
		"source-url",
		environmentOrDefault("SOURCE_URL", defaultSourceURL),
		"source repository URL used by OCI labels",
	)
	releaseParallelism = flag.Int(
		"parallelism",
		1,
		"GoReleaser build parallelism",
	)
	releaseClean = flag.Bool(
		"clean",
		true,
		"remove previous GoReleaser output before building",
	)
)

type releaseConfig struct {
	RepoRoot        string
	DistDirectory   string
	VersionTag      string
	Version         string
	Commit          string
	RegistryImage   string
	DockerHubImage  string
	GoReleaserImage string
	SourceURL       string
	Parallelism     int
	Clean           bool
}

type releaseTargets struct {
	registryImage   string
	dockerHubImage  string
	goReleaserImage string
	sourceURL       string
}

var activeReleaseConfig *releaseConfig

func resolveReleaseConfig(a *goyek.A) (releaseConfig, error) {
	repositoryRoot, err := os.Getwd()
	if err != nil {
		return releaseConfig{}, fmt.Errorf("get repository root: %w", err)
	}
	versionTag, version, err := resolveReleaseVersion(a, repositoryRoot)
	if err != nil {
		return releaseConfig{}, err
	}

	commit := commandOutput(a, repositoryRoot, "git", "rev-parse", "HEAD")
	if commit == "" {
		return releaseConfig{}, errors.New("resolve release commit: empty Git output")
	}
	distDirectory, err := resolveDistDirectory(repositoryRoot)
	if err != nil {
		return releaseConfig{}, err
	}
	targets, err := resolveReleaseTargets()
	if err != nil {
		return releaseConfig{}, err
	}
	if *releaseParallelism < 1 {
		return releaseConfig{}, fmt.Errorf(
			"parallelism must be positive, got %d",
			*releaseParallelism,
		)
	}

	return releaseConfig{
		RepoRoot:        repositoryRoot,
		DistDirectory:   distDirectory,
		VersionTag:      versionTag,
		Version:         version,
		Commit:          commit,
		RegistryImage:   targets.registryImage,
		DockerHubImage:  targets.dockerHubImage,
		GoReleaserImage: targets.goReleaserImage,
		SourceURL:       targets.sourceURL,
		Parallelism:     *releaseParallelism,
		Clean:           *releaseClean,
	}, nil
}

func resolveReleaseVersion(
	a *goyek.A,
	repositoryRoot string,
) (string, string, error) {
	versionInput := strings.TrimSpace(*releaseVersion)
	if versionInput == "" {
		versionInput = commandOutput(
			a,
			repositoryRoot,
			"git",
			"describe",
			"--tags",
			"--exact-match",
		)
	}
	return normalizeVersion(versionInput)
}

func resolveDistDirectory(repositoryRoot string) (string, error) {
	distDirectory := strings.TrimSpace(*releaseDistDirectory)
	if distDirectory == "" {
		return "", errors.New("dist directory must not be empty")
	}
	if !filepath.IsAbs(distDirectory) {
		distDirectory = filepath.Join(repositoryRoot, distDirectory)
	}
	return filepath.Clean(distDirectory), nil
}

func resolveReleaseTargets() (releaseTargets, error) {
	registryImage, err := requiredReleaseValue(
		"registry image",
		strings.TrimRight(*releaseRegistryImage, "/"),
	)
	if err != nil {
		return releaseTargets{}, err
	}
	dockerHubImage, err := requiredReleaseValue(
		"docker hub image",
		strings.TrimRight(*releaseDockerHubImage, "/"),
	)
	if err != nil {
		return releaseTargets{}, err
	}
	goReleaserImage, err := requiredReleaseValue(
		"GoReleaser image",
		*releaseGoReleaserImage,
	)
	if err != nil {
		return releaseTargets{}, err
	}
	sourceURL, err := requiredReleaseValue(
		"source URL",
		strings.TrimRight(*releaseSourceURL, "/"),
	)
	if err != nil {
		return releaseTargets{}, err
	}
	return releaseTargets{
		registryImage:   registryImage,
		dockerHubImage:  dockerHubImage,
		goReleaserImage: goReleaserImage,
		sourceURL:       sourceURL,
	}, nil
}

func requiredReleaseValue(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", name)
	}
	return value, nil
}

func normalizeVersion(input string) (string, string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", "", errors.New("release version must not be empty")
	}
	version := strings.TrimPrefix(value, "v")
	parsed, err := semver.StrictNewVersion(version)
	if err != nil {
		return "", "", fmt.Errorf("invalid release version %q: %w", input, err)
	}
	version = parsed.String()
	return "v" + version, version, nil
}

func mustReleaseConfig(a *goyek.A) releaseConfig {
	a.Helper()
	if activeReleaseConfig == nil {
		a.Fatal("release-preflight did not initialize the release configuration")
		return releaseConfig{}
	}
	return *activeReleaseConfig
}

func environmentOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
