package config_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestLoadMavenGroupsFromHCL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "maven-groups.hcl")
	contents := []byte(`maven {
  central {
    registry = "https://repo.maven.apache.org/maven2"
  }

  spring {
    registry = "https://repo.spring.io/release"
  }
}

maven_groups {
  public {
    members = ["spring", "central"]
    fallback_on_error = true
    metadata_policy = "first"
  }
}
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write HCL config: %v", err)
	}

	cfg, err := config.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("load HCL config: %v", err)
	}
	group, ok := cfg.MavenGroup("public")
	if !ok {
		t.Fatal("Maven group public was not loaded")
	}
	if !slices.Equal(group.Members, []string{"spring", "central"}) {
		t.Fatalf("members = %v, want [spring central]", group.Members)
	}
	if !group.FallbackOnError {
		t.Fatal("fallback_on_error was not loaded")
	}
	if group.MetadataPolicy != config.MavenMetadataPolicyFirst {
		t.Fatalf("metadata policy = %q, want first", group.MetadataPolicy)
	}
}
