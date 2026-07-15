package main

import (
	"path/filepath"

	"github.com/goyek/goyek/v3"
)

const dockerContextName = "docker-context"

type dockerVariant struct {
	name       string
	dockerfile string
}

var (
	alpineVariant = dockerVariant{
		name:       "alpine",
		dockerfile: "docker/alpine.Dockerfile",
	}
	debianVariant = dockerVariant{
		name:       "debian",
		dockerfile: "docker/debian.Dockerfile",
	}
)

var prepareDockerContextTask = goyek.Define(goyek.Task{
	Name:  "prepare-docker-context",
	Usage: "prepare the multi-platform Docker build context",
	Deps:  goyek.Deps{releasePreflightTask},
	Action: func(a *goyek.A) {
		prepareDockerContext(a, mustReleaseConfig(a))
	},
})

var releaseAlpineTask = goyek.Define(goyek.Task{
	Name:     "release-alpine",
	Usage:    "publish Alpine-based multi-platform images",
	Deps:     goyek.Deps{prepareDockerContextTask},
	Parallel: true,
	Action: func(a *goyek.A) {
		publishDockerVariant(a, mustReleaseConfig(a), alpineVariant)
	},
})

var releaseDebianTask = goyek.Define(goyek.Task{
	Name:     "release-debian",
	Usage:    "publish Debian-based multi-platform images",
	Deps:     goyek.Deps{prepareDockerContextTask},
	Parallel: true,
	Action: func(a *goyek.A) {
		publishDockerVariant(a, mustReleaseConfig(a), debianVariant)
	},
})

var releaseImagesTask = goyek.Define(goyek.Task{
	Name:  "release-images",
	Usage: "publish all multi-platform container images",
	Deps:  goyek.Deps{releaseAlpineTask, releaseDebianTask},
})

func publishDockerVariant(
	a *goyek.A,
	config releaseConfig,
	variant dockerVariant,
) {
	tags := dockerTags(config, variant)
	labels := []string{
		"org.opencontainers.image.source=" + config.SourceURL,
		"org.opencontainers.image.version=" + config.Version,
		"org.opencontainers.image.revision=" + config.Commit,
	}
	arguments := make([]string, 0, 7+len(tags)*2+len(labels)*2+2)
	arguments = append(
		arguments,
		"buildx",
		"build",
		"--platform",
		"linux/amd64,linux/arm64",
		"--file",
		variant.dockerfile,
	)
	for _, tag := range tags {
		arguments = append(arguments, "--tag", tag)
	}
	for _, label := range labels {
		arguments = append(arguments, "--label", label)
	}
	arguments = append(
		arguments,
		"--push",
		dockerContextDirectory(config),
	)
	runCommand(a, "docker", arguments)
}

func dockerTags(config releaseConfig, variant dockerVariant) []string {
	suffix := ""
	if variant.name != alpineVariant.name {
		suffix = "-" + variant.name
	}

	repositories := []string{
		config.RegistryImage,
		config.DockerHubImage,
	}
	tags := make([]string, 0, len(repositories)*2)
	for _, repository := range repositories {
		tags = append(
			tags,
			repository+":"+config.Version+suffix,
			repository+":latest"+suffix,
		)
	}
	return tags
}

func dockerContextDirectory(config releaseConfig) string {
	return filepath.Join(config.DistDirectory, dockerContextName)
}
