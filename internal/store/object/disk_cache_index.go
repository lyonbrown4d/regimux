package object

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *DiskCacheStore) loadDiskCacheEntries() error {
	if err := s.loadMetadataEntries(); err != nil {
		return fmt.Errorf("load disk cache metadata: %w", err)
	}
	return removeOrphanCacheBodies(s.rootFS)
}

func (s *DiskCacheStore) loadMetadataEntries() error {
	return visitCacheDirectory(s.rootFS, "objects", func(file os.DirEntry) error {
		return s.loadMetadataEntryForFile(file)
	})
}

func (s *DiskCacheStore) loadMetadataEntryForFile(file os.DirEntry) error {
	stem, ok := cacheFileStemFromName(file.Name(), ".json")
	if !ok {
		return nil
	}
	if err := s.loadDiskCacheEntry(s.rootFS, stem); err == nil {
		return nil
	}
	s.logger().Warn("discarding invalid disk cache metadata", "result", "invalid")
	if err := s.removeManagedPair(s.rootFS, stem); err != nil {
		return fmt.Errorf("discard invalid disk cache entry: %w", err)
	}
	return nil
}

func removeOrphanCacheBodies(root *os.Root) error {
	return visitCacheDirectory(root, "objects", func(file os.DirEntry) error {
		return removeOrphanCacheBody(root, file)
	})
}

func removeOrphanCacheBody(root *os.Root, file os.DirEntry) error {
	stem, ok := cacheFileStemFromName(file.Name(), ".body")
	if !ok {
		return nil
	}
	metadataPath := filepath.Join("objects", stem+".json")
	metadataErr := inspectOrphanMetadata(root, metadataPath)
	if metadataErr == nil {
		return nil
	}
	if !errors.Is(metadataErr, os.ErrNotExist) {
		return metadataErr
	}
	return removeOrphanBodyFile(root, filepath.Join("objects", stem+".body"))
}

func inspectOrphanMetadata(root *os.Root, path string) error {
	_, err := root.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect orphan disk cache metadata: %w", err)
	}
	if err != nil {
		return fmt.Errorf("inspect orphan disk cache metadata: %w", err)
	}
	return nil
}

func removeOrphanBodyFile(root *os.Root, path string) error {
	info, err := root.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect orphan disk cache body: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if removeErr := root.Remove(path); removeErr != nil {
		return fmt.Errorf("remove orphan disk cache body: %w", removeErr)
	}
	return nil
}

func cacheFileStemFromName(name, extension string) (string, bool) {
	if !strings.HasSuffix(name, extension) {
		return "", false
	}
	stem := strings.TrimSuffix(name, extension)
	return stem, validCacheFileStem(stem)
}

func (s *DiskCacheStore) loadDiskCacheEntry(root *os.Root, stem string) error {
	data, err := readDiskCacheMetadata(root, filepath.Join("objects", stem+".json"))
	if err != nil {
		return err
	}
	metadata, err := decodeDiskCacheMetadata(data, stem)
	if err != nil {
		return err
	}
	if validateErr := validateCachedBody(root, stem, metadata); validateErr != nil {
		return validateErr
	}
	return s.restoreDiskCacheEntry(stem, metadata)
}

func readDiskCacheMetadata(root *os.Root, path string) ([]byte, error) {
	info, err := root.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect cache metadata: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("metadata is not a regular file")
	}
	if info.Size() > 64<<10 {
		return nil, errors.New("cache metadata exceeds limit")
	}
	file, err := root.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open cache metadata: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, errors.Join(fmt.Errorf("read cache metadata: %w", readErr), wrapDiskCacheClose("close cache metadata", closeErr))
	}
	if closeErr != nil {
		return nil, wrapDiskCacheClose("close cache metadata", closeErr)
	}
	if len(data) > 64<<10 {
		return nil, errors.New("cache metadata exceeds limit")
	}
	return data, nil
}

func decodeDiskCacheMetadata(data []byte, stem string) (diskCacheMetadata, error) {
	var metadata diskCacheMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return diskCacheMetadata{}, fmt.Errorf("decode cache metadata: %w", err)
	}
	normalized, err := normalizeDigest(metadata.Digest)
	if err != nil || normalized != metadata.Digest || cacheFileStem(normalized) != stem || metadata.Size < 0 {
		return diskCacheMetadata{}, errors.New("metadata identity is invalid")
	}
	return metadata, nil
}

func validateCachedBody(root *os.Root, stem string, metadata diskCacheMetadata) error {
	bodyPath := filepath.Join("objects", stem+".body")
	bodyInfo, err := root.Lstat(bodyPath)
	if err != nil {
		return fmt.Errorf("inspect cached body: %w", err)
	}
	if bodyInfo.Mode()&os.ModeSymlink != 0 || !bodyInfo.Mode().IsRegular() ||
		bodyInfo.Size() != metadata.Size || bodyInfo.ModTime().UnixNano() != metadata.FileModTime {
		return errors.New("cached body is missing or invalid")
	}
	return nil
}

func (s *DiskCacheStore) restoreDiskCacheEntry(stem string, metadata diskCacheMetadata) error {
	lastAccess := time.Unix(0, metadata.LastAccess)
	if metadata.LastAccess == 0 {
		lastAccess = time.Unix(0, metadata.StoredAt)
	}
	if lastAccess.IsZero() || time.Since(lastAccess) > s.opts.TTL {
		return errors.New("cached entry expired")
	}
	if metadata.Size > s.opts.MaxObjectBytes || metadata.Size > s.opts.MaxBytes || !s.makeRoomLocked(metadata.Size) {
		return errors.New("cached entry exceeds configured bounds")
	}
	s.entries[metadata.Digest] = &diskCacheEntry{
		metadata:   metadata,
		bodyPath:   s.bodyPath(stem),
		metaPath:   s.metadataPath(stem),
		lastAccess: lastAccess,
	}
	s.lru.Add(metadata.Digest, s.entries[metadata.Digest])
	s.bytes += metadata.Size
	return nil
}

func (s *DiskCacheStore) removeManagedPair(root *os.Root, stem string) error {
	if !validCacheFileStem(stem) {
		return errors.New("invalid managed cache file name")
	}
	if err := removeDiskCachePair(root, filepath.Join("objects", stem+".json"), filepath.Join("objects", stem+".body")); err != nil {
		return fmt.Errorf("remove invalid cache entry: %w", err)
	}
	return nil
}

func removeDiskCachePair(root *os.Root, metadataPath, bodyPath string) error {
	var result error
	for _, path := range []string{metadataPath, bodyPath} {
		if err := removeManagedRegularFile(root, path); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func removeManagedRegularFile(root *os.Root, path string) error {
	info, err := root.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect managed cache file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if removeErr := root.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return fmt.Errorf("remove managed cache file: %w", removeErr)
	}
	return nil
}

func visitCacheDirectory(root *os.Root, path string, visit func(os.DirEntry) error) (result error) {
	directory, err := root.Open(path)
	if err != nil {
		return fmt.Errorf("open managed cache directory: %w", err)
	}
	defer func() {
		result = errors.Join(result, wrapDiskCacheClose("close managed cache directory", directory.Close()))
	}()
	return visitCacheEntries(directory, visit)
}

func visitCacheEntries(directory *os.File, visit func(os.DirEntry) error) error {
	for {
		entries, readErr := directory.ReadDir(128)
		for _, entry := range entries {
			if err := visit(entry); err != nil {
				return fmt.Errorf("visit managed cache directory entry: %w", err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("read managed cache directory: %w", readErr)
		}
	}
}

func (s *DiskCacheStore) bodyPath(stem string) string {
	return filepath.Join("objects", stem+".body")
}

func (s *DiskCacheStore) metadataPath(stem string) string {
	return filepath.Join("objects", stem+".json")
}

func cacheFileStem(digest string) string {
	hash := sha256.Sum256([]byte(digest))
	return hex.EncodeToString(hash[:])
}

func validCacheFileStem(stem string) bool {
	if len(stem) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(stem)
	return err == nil
}
