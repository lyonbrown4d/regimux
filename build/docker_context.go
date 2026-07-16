package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type dockerContextCopy struct {
	sourceRoot  dockerContextSource
	source      string
	destination string
}

func prepareDockerContext(a interface {
	Fatal(args ...any)
	Logf(format string, args ...any)
}, config releaseConfig) {
	contextDirectory, err := createDockerContext(config)
	if err != nil {
		a.Fatal(err)
		return
	}
	a.Logf("prepared Docker context at %s", contextDirectory)
}

func createDockerContext(config releaseConfig) (_ string, err error) {
	mkdirErr := os.MkdirAll(config.DistDirectory, 0o750)
	if mkdirErr != nil {
		return "", fmt.Errorf("create dist directory: %w", mkdirErr)
	}

	distRoot, err := os.OpenRoot(config.DistDirectory)
	if err != nil {
		return "", fmt.Errorf("open dist directory: %w", err)
	}
	defer func() {
		err = closeWithError(err, distRoot)
	}()

	repositoryRoot, err := os.OpenRoot(config.RepoRoot)
	if err != nil {
		return "", fmt.Errorf("open repository root: %w", err)
	}
	defer func() {
		err = closeWithError(err, repositoryRoot)
	}()

	temporaryName, populateErr := populateDockerContext(
		config,
		distRoot,
		repositoryRoot,
	)
	activated := false
	if temporaryName != "" {
		defer func() {
			err = errors.Join(
				err,
				cleanupTemporaryContext(
					distRoot,
					temporaryName,
					activated,
				),
			)
		}()
	}
	if populateErr != nil {
		return "", populateErr
	}
	if err := activateDockerContext(distRoot, temporaryName); err != nil {
		return "", err
	}
	activated = true
	return dockerContextDirectory(config), nil
}

func populateDockerContext(
	config releaseConfig,
	distRoot *os.Root,
	repositoryRoot *os.Root,
) (temporaryName string, err error) {
	temporaryDirectory, err := os.MkdirTemp(
		config.DistDirectory,
		".docker-context-",
	)
	if err != nil {
		return "", fmt.Errorf("create temporary Docker context: %w", err)
	}
	temporaryName = filepath.Base(temporaryDirectory)

	temporaryRoot, err := distRoot.OpenRoot(temporaryName)
	if err != nil {
		return temporaryName, fmt.Errorf("open temporary Docker context: %w", err)
	}
	defer func() {
		err = closeWithError(err, temporaryRoot)
	}()

	if err := copyDockerContextFiles(
		distRoot,
		repositoryRoot,
		temporaryRoot,
	); err != nil {
		return temporaryName, err
	}
	return temporaryName, nil
}

func copyDockerContextFiles(
	distRoot dockerContextSource,
	repositoryRoot dockerContextSource,
	destinationRoot dockerContextDestination,
) error {
	copies := []dockerContextCopy{
		{
			sourceRoot: distRoot,
			source: filepath.Join(
				"regimuxd-linux_linux_amd64_v1",
				"regimuxd",
			),
			destination: filepath.Join("linux", "amd64", "regimuxd"),
		},
		{
			sourceRoot: distRoot,
			source: filepath.Join(
				"regimuxd-linux_linux_arm64_v8.0",
				"regimuxd",
			),
			destination: filepath.Join("linux", "arm64", "regimuxd"),
		},
		{
			sourceRoot: repositoryRoot,
			source:     filepath.Join("configs", "regimux.minimal.hcl"),
			destination: filepath.Join(
				"configs",
				"regimux.minimal.hcl",
			),
		},
	}
	for _, copy := range copies {
		if err := copyRootFile(
			copy.sourceRoot,
			copy.source,
			destinationRoot,
			copy.destination,
		); err != nil {
			return err
		}
	}
	return nil
}

func copyRootFile(
	sourceRoot dockerContextSource,
	source string,
	destinationRoot dockerContextDestination,
	destination string,
) (err error) {
	input, err := sourceRoot.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer func() {
		err = closeWithError(err, input)
	}()

	mkdirErr := destinationRoot.MkdirAll(
		filepath.Dir(destination),
		0o750,
	)
	if mkdirErr != nil {
		return fmt.Errorf("create directory for %s: %w", destination, mkdirErr)
	}
	output, err := destinationRoot.OpenFile(
		destination,
		os.O_CREATE|os.O_TRUNC|os.O_WRONLY,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("create %s: %w", destination, err)
	}
	defer func() {
		err = closeWithError(err, output)
	}()

	if _, err := io.Copy(output, input); err != nil {
		return fmt.Errorf("copy %s to %s: %w", source, destination, err)
	}
	return nil
}

func activateDockerContext(
	distRoot dockerContextActivator,
	temporaryName string,
) error {
	if err := distRoot.RemoveAll(dockerContextName); err != nil {
		return fmt.Errorf("remove previous Docker context: %w", err)
	}
	if err := distRoot.Rename(temporaryName, dockerContextName); err != nil {
		return fmt.Errorf("activate Docker context: %w", err)
	}
	return nil
}

func cleanupTemporaryContext(
	distRoot dockerContextRemover,
	temporaryName string,
	activated bool,
) error {
	if activated {
		return nil
	}
	if err := distRoot.RemoveAll(temporaryName); err != nil {
		return fmt.Errorf("remove temporary Docker context: %w", err)
	}
	return nil
}

func closeWithError(current error, closer io.Closer) error {
	return errors.Join(current, closer.Close())
}
