package object

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func rejectSymlinkAncestors(path string) error {
	volume := filepath.VolumeName(path)
	remainder := strings.TrimPrefix(path, volume)
	remainder = strings.Trim(remainder, string(os.PathSeparator))
	current := volume + string(os.PathSeparator)
	parts := strings.Split(remainder, string(os.PathSeparator))
	for index, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		missing, err := inspectCachePathComponent(current, index < len(parts)-1)
		if err != nil {
			return err
		}
		if missing {
			return nil
		}
	}
	return nil
}

func inspectCachePathComponent(path string, requireDirectory bool) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect disk cache path component: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("disk cache path contains a symlink")
	}
	if requireDirectory && !info.IsDir() {
		return false, errors.New("disk cache path component is not a directory")
	}
	return false, nil
}

func inspectCacheRoot(root string) (os.FileInfo, error) {
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open disk cache directory: %w", err)
	}
	info, statErr := rootFS.Lstat(".")
	closeErr := rootFS.Close()
	if statErr != nil {
		return nil, errors.Join(fmt.Errorf("inspect disk cache directory: %w", statErr), wrapDiskCacheClose("close disk cache directory", closeErr))
	}
	if !info.IsDir() {
		return nil, errors.Join(errors.New("disk cache path must be a directory"), wrapDiskCacheClose("close disk cache directory", closeErr))
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("resolve disk cache path: %w", err), wrapDiskCacheClose("close disk cache directory", closeErr))
	}
	if filepath.Clean(resolved) != filepath.Clean(root) {
		return nil, errors.Join(errors.New("disk cache path contains a symlink"), wrapDiskCacheClose("close disk cache directory", closeErr))
	}
	if closeErr != nil {
		return nil, wrapDiskCacheClose("close disk cache directory", closeErr)
	}
	return info, nil
}
