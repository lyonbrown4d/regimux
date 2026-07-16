package main

import (
	"strings"
	"testing"
)

func TestValidateGitHubRelease(t *testing.T) {
	t.Parallel()

	config := releaseConfig{VersionTag: "v1.2.11", Version: "1.2.11"}
	assets := expectedReleaseAssets(config.Version)
	releaseAssets := make([]githubReleaseAsset, 0, len(assets))
	for _, name := range assets {
		releaseAssets = append(releaseAssets, githubReleaseAsset{Name: name})
	}

	tests := []struct {
		name    string
		release githubReleaseView
		wantErr string
	}{
		{
			name: "complete release",
			release: githubReleaseView{
				TagName: config.VersionTag,
				Assets:  releaseAssets,
			},
		},
		{
			name: "draft",
			release: githubReleaseView{
				TagName: config.VersionTag,
				IsDraft: true,
				Assets:  releaseAssets,
			},
			wantErr: "draft",
		},
		{
			name: "missing asset",
			release: githubReleaseView{
				TagName: config.VersionTag,
				Assets:  releaseAssets[1:],
			},
			wantErr: "checksums.txt",
		},
		{
			name: "wrong tag",
			release: githubReleaseView{
				TagName: "v1.2.10",
				Assets:  releaseAssets,
			},
			wantErr: "want",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertErrorContains(
				t,
				validateGitHubRelease(config, test.release),
				test.wantErr,
			)
		})
	}
}

func TestValidateImageIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		index   string
		wantErr string
	}{
		{
			name:  "required platforms and attestations",
			index: "{\"manifests\":[{\"platform\":{\"os\":\"linux\",\"architecture\":\"amd64\"}},{\"platform\":{\"os\":\"linux\",\"architecture\":\"arm64\"}},{\"platform\":{\"os\":\"unknown\",\"architecture\":\"unknown\"}}]}",
		},
		{
			name:    "missing arm64",
			index:   "{\"manifests\":[{\"platform\":{\"os\":\"linux\",\"architecture\":\"amd64\"}}]}",
			wantErr: "linux/arm64",
		},
		{
			name:    "invalid JSON",
			index:   "{",
			wantErr: "decode",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertErrorContains(
				t,
				validateImageIndex([]byte(test.index)),
				test.wantErr,
			)
		})
	}
}

func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()

	if want == "" {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want substring %q", err, want)
	}
}

func TestExpectedReleaseAssets(t *testing.T) {
	t.Parallel()

	assets := expectedReleaseAssets("1.2.11")
	if len(assets) != 13 {
		t.Fatalf("asset count = %d, want 13", len(assets))
	}
}
