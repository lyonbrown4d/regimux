package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goyek/goyek/v3"
	"github.com/goyek/x/boot"
)

func main() {
	root, err := findRepositoryRoot()
	if err != nil {
		panic(err)
	}
	if err := os.Chdir(root); err != nil {
		panic(err)
	}

	goyek.SetDefault(releaseTask)
	boot.Main()
}

func findRepositoryRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get current directory: %w", err)
	}
	return findRepositoryRootFrom(current)
}

func findRepositoryRootFrom(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve start directory %q: %w", start, err)
	}

	for {
		if hasRepositoryMarkers(current) {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("repository root not found from %q", start)
		}
		current = parent
	}
}

func hasRepositoryMarkers(directory string) bool {
	for _, name := range []string{"go.mod", ".goreleaser.yaml"} {
		info, err := os.Stat(filepath.Join(directory, name))
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
}
