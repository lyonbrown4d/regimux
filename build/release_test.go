package main

import (
	"slices"
	"testing"
)

func TestNormalizeVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantTag     string
		wantVersion string
	}{
		{
			name:        "tag",
			input:       "v1.2.9",
			wantTag:     "v1.2.9",
			wantVersion: "1.2.9",
		},
		{
			name:        "bare version",
			input:       "1.3.0-rc.1",
			wantTag:     "v1.3.0-rc.1",
			wantVersion: "1.3.0-rc.1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			tag, version, err := normalizeVersion(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if tag != test.wantTag {
				t.Errorf("tag = %q, want %q", tag, test.wantTag)
			}
			if version != test.wantVersion {
				t.Errorf(
					"version = %q, want %q",
					version,
					test.wantVersion,
				)
			}
		})
	}
}

func TestNormalizeVersionRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	for _, input := range []string{" ", "release-1.2.9"} {
		if _, _, err := normalizeVersion(input); err == nil {
			t.Errorf("normalizeVersion(%q) error = nil", input)
		}
	}
}

func TestRemoteTagCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		output  string
		want    string
		wantErr bool
	}{
		{
			name:   "annotated tag",
			output: "tag-object\trefs/tags/v1.2.11\nrelease-commit\trefs/tags/v1.2.11^{}",
			want:   "release-commit",
		},
		{
			name:   "lightweight tag",
			output: "release-commit\trefs/tags/v1.2.11",
			want:   "release-commit",
		},
		{
			name:    "missing tag",
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertRemoteTagCommit(t, test.output, test.want, test.wantErr)
		})
	}
}

func assertRemoteTagCommit(
	t *testing.T,
	output string,
	want string,
	wantErr bool,
) {
	t.Helper()

	actual, err := remoteTagCommit(output, "v1.2.11")
	if wantErr {
		if err == nil {
			t.Fatal("remoteTagCommit() error = nil")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if actual != want {
		t.Fatalf("remoteTagCommit() = %q, want %q", actual, want)
	}
}

func TestGoReleaserDockerArguments(t *testing.T) {
	t.Parallel()

	config := releaseConfig{
		RepoRoot:        "/repo",
		GoReleaserImage: defaultGoReleaserImage,
		GoToolchain:     defaultGoToolchain,
		Parallelism:     1,
		Clean:           true,
	}
	arguments := goReleaserDockerArguments(config)
	if !slices.Contains(arguments, "GOTOOLCHAIN") {
		t.Fatal("GoReleaser Docker arguments do not forward GOTOOLCHAIN")
	}
	if !slices.Contains(arguments, "goreleaser/goreleaser:v2.17.0") {
		t.Fatalf("GoReleaser image missing from arguments: %v", arguments)
	}
	if !slices.Contains(arguments, "--clean") {
		t.Fatal("GoReleaser Docker arguments do not request a clean build")
	}
}
