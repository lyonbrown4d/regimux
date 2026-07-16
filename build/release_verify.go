package main

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/goyek/goyek/v3"
)

type githubReleaseView struct {
	TagName string
	IsDraft bool
	Assets  []githubReleaseAsset
}

type githubReleaseAsset struct {
	Name string
}

type imageIndex struct {
	Manifests []imageManifest
}

type imageManifest struct {
	Platform imagePlatform
}

type imagePlatform struct {
	OS           string
	Architecture string
}

func verifyRelease(a *goyek.A, config releaseConfig) {
	token := resolveGitHubToken(a, config.RepoRoot)
	releaseJSON := commandOutputWithEnvironment(
		a,
		config.RepoRoot,
		"gh",
		[]string{
			"release",
			"view",
			config.VersionTag,
			"--json",
			"tagName,isDraft,assets",
		},
		secretCommandEnvironment("GH_TOKEN", token),
		secretCommandEnvironment("GITHUB_TOKEN", token),
	)

	var release githubReleaseView
	if err := json.Unmarshal([]byte(releaseJSON), &release); err != nil {
		a.Fatalf("decode GitHub release: %v", err)
		return
	}
	if err := validateGitHubRelease(config, release); err != nil {
		a.Fatal(err)
		return
	}

	for _, reference := range releaseImageReferences(config) {
		rawIndex := commandOutputWithEnvironment(
			a,
			config.RepoRoot,
			"docker",
			[]string{"buildx", "imagetools", "inspect", "--raw", reference},
		)
		if err := validateImageIndex([]byte(rawIndex)); err != nil {
			a.Fatalf("verify image %s: %v", reference, err)
			return
		}
		a.Logf("verified image %s", reference)
	}
	a.Logf(
		"verified release %s with %d required assets",
		config.VersionTag,
		len(expectedReleaseAssets(config.Version)),
	)
}

func validateGitHubRelease(
	config releaseConfig,
	release githubReleaseView,
) error {
	if release.TagName != config.VersionTag {
		return fmt.Errorf(
			"GitHub release tag = %q, want %q",
			release.TagName,
			config.VersionTag,
		)
	}
	if release.IsDraft {
		return fmt.Errorf("GitHub release %s is still a draft", config.VersionTag)
	}

	actualAssets := make(map[string]struct{}, len(release.Assets))
	for _, asset := range release.Assets {
		actualAssets[asset.Name] = struct{}{}
	}
	for _, expected := range expectedReleaseAssets(config.Version) {
		if _, ok := actualAssets[expected]; !ok {
			return fmt.Errorf(
				"GitHub release %s is missing asset %s",
				config.VersionTag,
				expected,
			)
		}
	}
	return nil
}

func expectedReleaseAssets(version string) []string {
	prefix := "regimux_" + version + "_"
	packagePrefix := "regimuxd_" + version + "_"
	return []string{
		"checksums.txt",
		packagePrefix + "linux_amd64.deb",
		packagePrefix + "linux_amd64.rpm",
		packagePrefix + "linux_arm64.deb",
		packagePrefix + "linux_arm64.rpm",
		prefix + "darwin_amd64.tar.gz",
		prefix + "darwin_arm64.tar.gz",
		prefix + "linux_amd64.tar.gz",
		prefix + "linux_arm64.tar.gz",
		prefix + "windows_amd64.exe",
		prefix + "windows_amd64.zip",
		prefix + "windows_arm64.exe",
		prefix + "windows_arm64.zip",
	}
}

func releaseImageReferences(config releaseConfig) []string {
	return []string{
		config.RegistryImage + ":" + config.Version,
		config.RegistryImage + ":" + config.Version + "-debian",
		config.DockerHubImage + ":" + config.Version,
		config.DockerHubImage + ":" + config.Version + "-debian",
	}
}

func validateImageIndex(data []byte) error {
	var index imageIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("decode image index: %w", err)
	}

	platforms := make([]string, 0, len(index.Manifests))
	for _, manifest := range index.Manifests {
		if manifest.Platform.OS == "" || manifest.Platform.Architecture == "" {
			continue
		}
		platforms = append(
			platforms,
			manifest.Platform.OS+"/"+manifest.Platform.Architecture,
		)
	}
	for _, required := range []string{"linux/amd64", "linux/arm64"} {
		if !slices.Contains(platforms, required) {
			return fmt.Errorf("image index is missing platform %s", required)
		}
	}
	return nil
}
