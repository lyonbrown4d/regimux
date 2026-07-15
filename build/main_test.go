package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRepositoryRootFrom(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, name := range []string{"go.mod", ".goreleaser.yaml"} {
		if err := os.WriteFile(
			filepath.Join(root, name),
			[]byte(name),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
	}

	nested := filepath.Join(root, "build", "nested")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatal(err)
	}

	actual, err := findRepositoryRootFrom(nested)
	if err != nil {
		t.Fatal(err)
	}
	if actual != root {
		t.Fatalf("repository root = %q, want %q", actual, root)
	}
}
