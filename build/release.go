package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/goyek/goyek/v3"
	goyekcmd "github.com/goyek/x/cmd"
)

var releasePreflightTask = goyek.Define(goyek.Task{
	Name:  "release-preflight",
	Usage: "resolve and validate release inputs",
	Action: func(a *goyek.A) {
		config, err := resolveReleaseConfig(a)
		if err != nil {
			a.Fatal(err)
			return
		}
		activeReleaseConfig = &config
		a.Logf("release %s from %s", config.VersionTag, config.Commit)
	},
})

var releaseArtifactsTask = goyek.Define(goyek.Task{
	Name:  "release-artifacts",
	Usage: "publish archives, packages, checksums, and the GitHub release",
	Deps:  goyek.Deps{releasePreflightTask},
	Action: func(a *goyek.A) {
		publishReleaseArtifacts(a, mustReleaseConfig(a))
	},
})

var releaseTask = goyek.Define(goyek.Task{
	Name:  "release",
	Usage: "validate and publish all release artifacts and images",
	Deps: goyek.Deps{
		validateTask,
		releaseArtifactsTask,
		releaseImagesTask,
	},
})

func publishReleaseArtifacts(a *goyek.A, config releaseConfig) {
	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if token == "" {
		token = commandOutput(a, config.RepoRoot, "gh", "auth", "token")
	}
	if token == "" {
		a.Fatal("GitHub token is empty")
		return
	}

	dockerArguments := []string{
		"run",
		"--rm",
		"--volume",
		config.RepoRoot + ":/work",
		"--workdir",
		"/work",
		"--env",
		"GITHUB_TOKEN",
		"--env",
		"REGISTRY_IMAGE",
		"--env",
		"DOCKERHUB_IMAGE",
		config.GoReleaserImage,
		"release",
		"--parallelism",
		strconv.Itoa(config.Parallelism),
		"--skip=docker",
	}
	if config.Clean {
		dockerArguments = append(dockerArguments, "--clean")
	}

	runCommand(
		a,
		"docker",
		dockerArguments,
		goyekcmd.Env("GITHUB_TOKEN", token),
		goyekcmd.Env("REGISTRY_IMAGE", config.RegistryImage),
		goyekcmd.Env("DOCKERHUB_IMAGE", config.DockerHubImage),
	)
}
