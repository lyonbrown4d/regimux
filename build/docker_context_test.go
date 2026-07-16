package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type dockerContextCopyFixture struct {
	source          dockerContextSource
	destination     dockerContextDestination
	destinationRoot *os.Root
	outsideRoot     *os.Root
}

type dockerContextSourceEscapeTest struct {
	name            string
	source          string
	requiresSymlink bool
}

type dockerContextDestinationEscapeTest struct {
	name            string
	destination     string
	outsideName     string
	requiresSymlink bool
	wantPrefix      string
}

func TestCopyRootFileRejectsSourceEscapes(t *testing.T) {
	sandbox := t.TempDir()
	sourceDirectory := filepath.Join(sandbox, "source")
	destinationDirectory := filepath.Join(sandbox, "destination")
	outsideDirectory := filepath.Join(sandbox, "outside")
	makeDockerContextTestDirectories(
		t,
		sourceDirectory,
		destinationDirectory,
		outsideDirectory,
	)
	writeDockerContextTestFile(t, outsideDirectory, "secret", "outside")
	symlinkErr := os.Symlink(
		filepath.Join("..", "outside"),
		filepath.Join(sourceDirectory, "escape"),
	)
	fixture := newDockerContextCopyFixture(
		t,
		sourceDirectory,
		destinationDirectory,
		outsideDirectory,
	)
	tests := []dockerContextSourceEscapeTest{
		{
			name:   "traversal",
			source: filepath.Join("..", "outside", "secret"),
		},
		{
			name:   "absolute path",
			source: filepath.Join(outsideDirectory, "secret"),
		},
		{
			name:            "symlink escape",
			source:          filepath.Join("escape", "secret"),
			requiresSymlink: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.requiresSymlink && symlinkErr != nil {
				t.Skipf("symlinks are not supported: %v", symlinkErr)
			}
			assertCopyRootFileRejectsSourceEscape(t, fixture, test)
		})
	}
}

func TestCopyRootFileRejectsDestinationEscapes(t *testing.T) {
	sandbox := t.TempDir()
	sourceDirectory := filepath.Join(sandbox, "source")
	destinationDirectory := filepath.Join(sandbox, "destination")
	outsideDirectory := filepath.Join(sandbox, "outside")
	makeDockerContextTestDirectories(
		t,
		sourceDirectory,
		destinationDirectory,
		outsideDirectory,
	)
	writeDockerContextTestFile(t, sourceDirectory, "payload", "inside")
	writeDockerContextTestFile(t, outsideDirectory, "traversal", "outside")
	writeDockerContextTestFile(t, outsideDirectory, "absolute", "outside")
	writeDockerContextTestFile(t, outsideDirectory, "symlink", "outside")
	symlinkErr := os.Symlink(
		filepath.Join("..", "outside", "symlink"),
		filepath.Join(destinationDirectory, "escape"),
	)
	fixture := newDockerContextCopyFixture(
		t,
		sourceDirectory,
		destinationDirectory,
		outsideDirectory,
	)
	tests := []dockerContextDestinationEscapeTest{
		{
			name:        "traversal",
			destination: filepath.Join("..", "outside", "traversal"),
			outsideName: "traversal",
			wantPrefix:  "create directory for ",
		},
		{
			name:        "absolute path",
			destination: filepath.Join(outsideDirectory, "absolute"),
			outsideName: "absolute",
			wantPrefix:  "create directory for ",
		},
		{
			name:            "symlink escape",
			destination:     "escape",
			outsideName:     "symlink",
			requiresSymlink: true,
			wantPrefix:      "create ",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.requiresSymlink && symlinkErr != nil {
				t.Skipf("symlinks are not supported: %v", symlinkErr)
			}
			assertCopyRootFileRejectsDestinationEscape(t, fixture, test)
		})
	}
}

func TestActivateDockerContextReplacesPreparedContext(t *testing.T) {
	distDirectory := t.TempDir()
	currentDirectory := filepath.Join(distDirectory, dockerContextName)
	temporaryName := ".docker-context-test"
	temporaryDirectory := filepath.Join(distDirectory, temporaryName)
	makeDockerContextTestDirectories(
		t,
		currentDirectory,
		temporaryDirectory,
	)
	writeDockerContextTestFile(t, currentDirectory, "old", "old")
	writeDockerContextTestFile(t, temporaryDirectory, "new", "new")

	distRoot := openDockerContextTestRoot(t, distDirectory)
	var activator dockerContextActivator = distRoot
	if err := activateDockerContext(activator, temporaryName); err != nil {
		t.Fatalf("activateDockerContext() error = %v", err)
	}

	content, err := distRoot.ReadFile(
		filepath.Join(dockerContextName, "new"),
	)
	if err != nil {
		t.Fatalf("read active context marker: %v", err)
	}
	if string(content) != "new" {
		t.Fatalf("active context marker = %q, want %q", content, "new")
	}
	if _, err := distRoot.Stat(
		filepath.Join(dockerContextName, "old"),
	); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old context remains after activation: %v", err)
	}
	if _, err := distRoot.Stat(temporaryName); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary context remains after activation: %v", err)
	}
}

func assertCopyRootFileRejectsSourceEscape(
	t *testing.T,
	fixture dockerContextCopyFixture,
	test dockerContextSourceEscapeTest,
) {
	t.Helper()
	destination := filepath.Join("copied", test.name)
	err := copyRootFile(
		fixture.source,
		test.source,
		fixture.destination,
		destination,
	)
	if err == nil {
		t.Fatalf("copyRootFile(%q) succeeded, want escape rejection", test.source)
	}
	wantPrefix := "open " + test.source + ": "
	if !strings.HasPrefix(err.Error(), wantPrefix) {
		t.Fatalf(
			"copyRootFile(%q) error = %q, want prefix %q",
			test.source,
			err,
			wantPrefix,
		)
	}
	if _, err := fixture.destinationRoot.Stat(
		destination,
	); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination created after rejected source: %v", err)
	}
}

func assertCopyRootFileRejectsDestinationEscape(
	t *testing.T,
	fixture dockerContextCopyFixture,
	test dockerContextDestinationEscapeTest,
) {
	t.Helper()
	err := copyRootFile(
		fixture.source,
		"payload",
		fixture.destination,
		test.destination,
	)
	if err == nil {
		t.Fatalf(
			"copyRootFile(%q) succeeded, want escape rejection",
			test.destination,
		)
	}
	wantPrefix := test.wantPrefix + test.destination + ": "
	if !strings.HasPrefix(err.Error(), wantPrefix) {
		t.Fatalf(
			"copyRootFile(%q) error = %q, want prefix %q",
			test.destination,
			err,
			wantPrefix,
		)
	}
	content, err := fixture.outsideRoot.ReadFile(test.outsideName)
	if err != nil {
		t.Fatalf("read outside file after rejected destination: %v", err)
	}
	if string(content) != "outside" {
		t.Fatalf(
			"outside file after rejected destination = %q, want %q",
			content,
			"outside",
		)
	}
}

func newDockerContextCopyFixture(
	t *testing.T,
	sourceDirectory string,
	destinationDirectory string,
	outsideDirectory string,
) dockerContextCopyFixture {
	t.Helper()
	sourceRoot := openDockerContextTestRoot(t, sourceDirectory)
	destinationRoot := openDockerContextTestRoot(t, destinationDirectory)
	return dockerContextCopyFixture{
		source:          sourceRoot,
		destination:     destinationRoot,
		destinationRoot: destinationRoot,
		outsideRoot:     openDockerContextTestRoot(t, outsideDirectory),
	}
}

func makeDockerContextTestDirectories(t *testing.T, directories ...string) {
	t.Helper()
	for _, directory := range directories {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			t.Fatalf("create test directory %s: %v", directory, err)
		}
	}
}

func writeDockerContextTestFile(
	t *testing.T,
	directory string,
	name string,
	content string,
) {
	t.Helper()
	if err := os.WriteFile(
		filepath.Join(directory, name),
		[]byte(content),
		0o600,
	); err != nil {
		t.Fatalf("write test file %s: %v", name, err)
	}
}

func openDockerContextTestRoot(t *testing.T, directory string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("open test root %s: %v", directory, err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close test root %s: %v", directory, err)
		}
	})
	return root
}
