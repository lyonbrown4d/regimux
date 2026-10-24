package object

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
)

func prepareDiskCacheDirectory(directory string) (string, error) {
	root, err := filepath.Abs(filepath.Clean(directory))
	if err != nil {
		return "", fmt.Errorf("resolve disk cache directory: %w", err)
	}
	if filepath.Dir(root) == root {
		return "", errors.New("disk cache directory cannot be a filesystem root")
	}
	if prepareErr := createAndInspectCacheRoot(root); prepareErr != nil {
		return "", prepareErr
	}
	if secureErr := validateAndSecureCacheRoot(root); secureErr != nil {
		return "", secureErr
	}
	return root, nil
}

func createAndInspectCacheRoot(root string) error {
	if err := rejectSymlinkAncestors(root); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create disk cache directory: %w", err)
	}
	_, err := inspectCacheRoot(root)
	return err
}

func validateAndSecureCacheRoot(root string) error {
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("open disk cache directory: %w", err)
	}
	markerErr := validateRootMarker(rootFS)
	if markerErr == nil {
		if chmodErr := rootFS.Chmod(".", 0o700); chmodErr != nil {
			markerErr = fmt.Errorf("secure owned cache directory: %w", chmodErr)
		}
	}
	closeErr := rootFS.Close()
	if markerErr != nil || closeErr != nil {
		return errors.Join(markerErr, wrapDiskCacheClose("close disk cache directory", closeErr))
	}
	return nil
}

func wrapDiskCacheClose(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func wrapDiskCacheError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func validateRootMarker(root *os.Root) error {
	info, err := root.Lstat(diskCacheMarkerName)
	if errors.Is(err, os.ErrNotExist) {
		return validateUnmarkedRoot(root)
	}
	if err != nil {
		return fmt.Errorf("inspect disk cache marker: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() != int64(len(diskCacheMarkerValue)) {
		return errors.New("disk cache marker is not a regular file")
	}
	return readMarkerContents(root)
}

func validateUnmarkedRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return fmt.Errorf("open unmarked disk cache directory: %w", err)
	}
	entries, readErr := directory.ReadDir(2)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return errors.Join(fmt.Errorf("inspect unmarked disk cache directory: %w", readErr), wrapDiskCacheClose("close unmarked cache directory", closeErr))
	}
	if len(entries) == 0 {
		return wrapDiskCacheClose("close unmarked cache directory", closeErr)
	}
	if len(entries) != 1 || entries[0].Name() != ".disk-cache.lock" {
		return errors.Join(errors.New("unmarked disk cache directory must be empty or contain only its lock file"), wrapDiskCacheClose("close unmarked cache directory", closeErr))
	}
	lockInfo, err := root.Lstat(entries[0].Name())
	if err != nil || lockInfo.Mode()&os.ModeSymlink != 0 || !lockInfo.Mode().IsRegular() {
		return errors.Join(errors.New("unmarked disk cache lock is not a regular file"), wrapDiskCacheError("inspect unmarked disk cache lock", err), wrapDiskCacheClose("close unmarked cache directory", closeErr))
	}
	return wrapDiskCacheClose("close unmarked cache directory", closeErr)
}

func readMarkerContents(root *os.Root) error {
	file, err := root.Open(diskCacheMarkerName)
	if err != nil {
		return fmt.Errorf("read disk cache marker: %w", err)
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, int64(len(diskCacheMarkerValue))+1))
	closeErr := file.Close()
	if readErr != nil {
		return errors.Join(fmt.Errorf("read disk cache marker: %w", readErr), wrapDiskCacheClose("close disk cache marker", closeErr))
	}
	if closeErr != nil {
		return fmt.Errorf("close disk cache marker: %w", closeErr)
	}
	if string(contents) != diskCacheMarkerValue {
		return errors.New("disk cache directory marker is not recognized")
	}
	return nil
}

func lockDiskCacheDirectory(root string) (*flock.Flock, error) {
	lockPath := filepath.Join(root, ".disk-cache.lock")
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open disk cache directory for lock: %w", err)
	}
	info, statErr := rootFS.Lstat(".disk-cache.lock")
	closeErr := rootFS.Close()
	if statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, errors.Join(errors.New("disk cache lock path is not a regular file"), wrapDiskCacheClose("close lock inspection root", closeErr))
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, errors.Join(fmt.Errorf("inspect disk cache lock: %w", statErr), wrapDiskCacheClose("close lock inspection root", closeErr))
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close disk cache directory after lock inspection: %w", closeErr)
	}
	lock := flock.New(lockPath)
	locked, err := lock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("lock disk cache directory: %w", err)
	}
	if !locked {
		return nil, errors.New("disk cache directory is already in use")
	}
	return lock, nil
}

func (s *DiskCacheStore) initializeDirectory() error {
	if err := validateRootMarker(s.rootFS); err != nil {
		return err
	}
	if err := s.rootFS.Chmod(".", 0o700); err != nil {
		return fmt.Errorf("secure owned cache directory: %w", err)
	}
	if err := s.ensureMarker(s.rootFS); err != nil {
		return err
	}
	for _, directory := range []string{"objects", "temporary"} {
		if err := prepareManagedCacheDirectory(s.rootFS, directory); err != nil {
			return fmt.Errorf("create managed cache directory: %w", err)
		}
	}
	if err := s.removeOwnedTemporaryFiles(s.rootFS); err != nil {
		return err
	}
	return s.loadDiskCacheEntries()
}

func prepareManagedCacheDirectory(root *os.Root, path string) error {
	if err := root.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create managed cache directory: %w", err)
	}
	info, err := root.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect managed cache directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("managed cache directory must be a private real directory")
	}
	if err := root.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure managed cache directory: %w", err)
	}
	return nil
}

func (s *DiskCacheStore) ensureMarker(root *os.Root) error {
	file, err := root.OpenFile(diskCacheMarkerName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return validateExistingMarker(root)
	}
	if err != nil {
		return fmt.Errorf("create disk cache marker: %w", err)
	}
	return writeAndCloseMarker(root, file)
}

func validateExistingMarker(root *os.Root) error {
	info, inspectErr := root.Lstat(diskCacheMarkerName)
	if inspectErr != nil {
		return fmt.Errorf("inspect disk cache marker: %w", inspectErr)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() != int64(len(diskCacheMarkerValue)) {
		return errors.New("disk cache marker is not a regular file")
	}
	return readMarkerContents(root)
}

func writeAndCloseMarker(root *os.Root, file *os.File) error {
	if _, err := file.WriteString(diskCacheMarkerValue); err != nil {
		return cleanupFailedMarker(root, file, fmt.Errorf("write disk cache marker: %w", err))
	}
	if err := file.Sync(); err != nil {
		return cleanupFailedMarker(root, file, fmt.Errorf("sync disk cache marker: %w", err))
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close disk cache marker: %w", err)
	}
	return nil
}

func cleanupFailedMarker(root *os.Root, file *os.File, cause error) error {
	closeErr := file.Close()
	removeErr := root.Remove(diskCacheMarkerName)
	return errors.Join(cause, wrapDiskCacheClose("close disk cache marker", closeErr), wrapRemoveDiskCacheMarker(removeErr))
}

func (s *DiskCacheStore) removeOwnedTemporaryFiles(root *os.Root) error {
	err := visitCacheDirectory(root, "temporary", func(entry os.DirEntry) error {
		return removeAbandonedTemporaryFile(root, entry)
	})
	if err != nil {
		return fmt.Errorf("clean disk cache temporary files: %w", err)
	}
	return nil
}

func removeAbandonedTemporaryFile(root *os.Root, entry os.DirEntry) error {
	if !isOwnedTemporaryFile(entry.Name()) {
		return nil
	}
	info, err := entry.Info()
	if err != nil {
		return fmt.Errorf("inspect disk cache temporary file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	path := filepath.Join("temporary", entry.Name())
	if removeErr := root.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return fmt.Errorf("remove abandoned disk cache temporary file: %w", removeErr)
	}
	return nil
}

func isOwnedTemporaryFile(name string) bool {
	return strings.HasPrefix(name, "fill-") || strings.HasPrefix(name, "meta-")
}

func wrapRemoveDiskCacheMarker(err error) error {
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("remove incomplete disk cache marker: %w", err)
}
