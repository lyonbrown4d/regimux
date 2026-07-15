package main

import (
	"reflect"
	"testing"
)

func TestDockerTags(t *testing.T) {
	t.Parallel()

	config := releaseConfig{
		Version:        "1.2.9",
		RegistryImage:  "ghcr.io/example/regimux",
		DockerHubImage: "example/regimux",
	}
	tests := []struct {
		name    string
		variant dockerVariant
		want    []string
	}{
		{
			name:    "alpine",
			variant: alpineVariant,
			want: []string{
				"ghcr.io/example/regimux:1.2.9",
				"ghcr.io/example/regimux:latest",
				"example/regimux:1.2.9",
				"example/regimux:latest",
			},
		},
		{
			name:    "debian",
			variant: debianVariant,
			want: []string{
				"ghcr.io/example/regimux:1.2.9-debian",
				"ghcr.io/example/regimux:latest-debian",
				"example/regimux:1.2.9-debian",
				"example/regimux:latest-debian",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if actual := dockerTags(config, test.variant); !reflect.DeepEqual(
				actual,
				test.want,
			) {
				t.Fatalf("dockerTags() = %v, want %v", actual, test.want)
			}
		})
	}
}
