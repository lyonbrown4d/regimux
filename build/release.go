package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/goyek/goyek/v3"
)

var releasePreflightTask = goyek.Define(goyek.Task{
	Name:  "release-preflight",
	Usage: "run validation and verify local and remote release prerequisites",
	Deps:  goyek.Deps{validateTask},
	Action: func(a *goyek.A) {
		config, err := resolveReleaseConfig(a)
		if err != nil {
			a.Fatal(err)
			return
		}
		activeReleaseConfig = &config
		validateReleasePreflight(a, config)
		a.Logf("release %s from %s", config.VersionTag, config.Commit)
	},
})

func init() {
	goyek.Define(goyek.Task{
		Name:  "release-artifacts",
		Usage: "publish archives, packages, checksums, and the GitHub release",
		Deps:  goyek.Deps{releasePreflightTask},
		Action: func(a *goyek.A) {
			publishReleaseArtifacts(a, mustReleaseConfig(a))
		},
	})
	goyek.Define(goyek.Task{
		Name:  "release-verify",
		Usage: "verify GitHub assets and versioned multi-platform images",
		Deps:  goyek.Deps{releasePreflightTask},
		Action: func(a *goyek.A) {
			verifyRelease(a, mustReleaseConfig(a))
		},
	})
}

var releaseTask = goyek.Define(goyek.Task{
	Name:  "release",
	Usage: "validate, publish, and verify all release artifacts and images",
	Deps:  goyek.Deps{releasePreflightTask},
	Action: func(a *goyek.A) {
		config := mustReleaseConfig(a)
		publishReleaseArtifacts(a, config)
		prepareDockerContext(a, config)
		publishDockerVariant(a, config, alpineVariant)
		publishDockerVariant(a, config, debianVariant)
		verifyRelease(a, config)
	},
})

func validateReleasePreflight(a *goyek.A, config releaseConfig) {
	trackedChanges := commandOutput(
		a,
		config.RepoRoot,
		"git",
		"status",
		"--porcelain",
		"--untracked-files=no",
	)
	if trackedChanges != "" {
		a.Fatal("release worktree contains tracked changes")
		return
	}

	localTagCommit := commandOutput(
		a,
		config.RepoRoot,
		"git",
		"rev-list",
		"-n",
		"1",
		config.VersionTag,
	)
	if localTagCommit != config.Commit {
		a.Fatalf(
			"release tag %s points to %s, want HEAD %s",
			config.VersionTag,
			localTagCommit,
			config.Commit,
		)
		return
	}

	tagReference := "refs/tags/" + config.VersionTag
	remoteTags := commandOutput(
		a,
		config.RepoRoot,
		"git",
		"ls-remote",
		"--tags",
		"origin",
		tagReference,
		tagReference+"^{}",
	)
	remoteCommit, err := remoteTagCommit(remoteTags, config.VersionTag)
	if err != nil {
		a.Fatal(err)
		return
	}
	if remoteCommit != config.Commit {
		a.Fatalf(
			"remote release tag %s points to %s, want %s",
			config.VersionTag,
			remoteCommit,
			config.Commit,
		)
		return
	}

	if token := resolveGitHubToken(a, config.RepoRoot); token == "" {
		a.Fatal("GitHub token is empty")
		return
	}

	runCommand(a, "docker", []string{"info"})
	runCommand(a, "docker", []string{"buildx", "version"})
	goVersion := commandOutputWithEnvironment(
		a,
		config.RepoRoot,
		"docker",
		goReleaserToolchainArguments(config),
		publicCommandEnvironment("GOTOOLCHAIN", config.GoToolchain),
	)
	if goVersion == "" {
		a.Fatal("GoReleaser container returned an empty Go version")
		return
	}
	a.Logf("GoReleaser toolchain: %s", goVersion)
}

func publishReleaseArtifacts(a *goyek.A, config releaseConfig) {
	token := resolveGitHubToken(a, config.RepoRoot)
	if token == "" {
		a.Fatal("GitHub token is empty")
		return
	}

	runCommandWithEnvironment(
		a,
		config.RepoRoot,
		"docker",
		goReleaserDockerArguments(config),
		secretCommandEnvironment("GITHUB_TOKEN", token),
		publicCommandEnvironment("REGISTRY_IMAGE", config.RegistryImage),
		publicCommandEnvironment("DOCKERHUB_IMAGE", config.DockerHubImage),
		publicCommandEnvironment("GOTOOLCHAIN", config.GoToolchain),
	)
}

func resolveGitHubToken(a *goyek.A, repositoryRoot string) string {
	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if token == "" {
		token = commandOutput(a, repositoryRoot, "gh", "auth", "token")
	}
	return strings.TrimSpace(token)
}

func goReleaserDockerArguments(config releaseConfig) []string {
	arguments := []string{
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
		"--env",
		"GOTOOLCHAIN",
		config.GoReleaserImage,
		"release",
		"--parallelism",
		strconv.Itoa(config.Parallelism),
		"--skip=docker",
	}
	if config.Clean {
		arguments = append(arguments, "--clean")
	}
	return arguments
}

func goReleaserToolchainArguments(config releaseConfig) []string {
	return []string{
		"run",
		"--rm",
		"--volume",
		config.RepoRoot + ":/work",
		"--workdir",
		"/work",
		"--env",
		"GOTOOLCHAIN",
		"--entrypoint",
		"go",
		config.GoReleaserImage,
		"env",
		"GOVERSION",
	}
}

func remoteTagCommit(output, versionTag string) (string, error) {
	tagReference := "refs/tags/" + versionTag
	var directCommit string
	for line := range strings.Lines(output) {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[1] {
		case tagReference + "^{}":
			return fields[0], nil
		case tagReference:
			directCommit = fields[0]
		}
	}
	if directCommit != "" {
		return directCommit, nil
	}
	return "", fmt.Errorf("remote release tag %s does not exist", versionTag)
}
