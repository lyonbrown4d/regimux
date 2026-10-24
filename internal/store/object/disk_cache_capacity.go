package object

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (s *DiskCacheStore) makeRoomLocked(size int64) bool {
	return s.makeRoomLockedWithFillSlot(size, false)
}

func (s *DiskCacheStore) makeRoomForExistingFillLocked(size int64) bool {
	return s.makeRoomLockedWithFillSlot(size, true)
}

func (s *DiskCacheStore) makeRoomLockedWithFillSlot(size int64, hasFillSlot bool) bool {
	extraEntries := 1
	if hasFillSlot {
		extraEntries = 0
	}
	for s.bytes+s.reserved+s.orphanedBytes+size > s.opts.MaxBytes || len(s.entries)+len(s.fills)+extraEntries > s.opts.MaxEntries {
		candidateDigest, candidate := s.oldestUnpinnedEntryLocked()
		if candidate == nil {
			return false
		}
		if err := s.removeEntryLocked(candidateDigest, candidate); err != nil {
			s.logger().Warn("could not evict disk cache entry", "result", "cleanup_error")
			return false
		}
	}
	return true
}

func (s *DiskCacheStore) oldestUnpinnedEntryLocked() (string, *diskCacheEntry) {
	for attempts := s.lru.Len(); attempts > 0; attempts-- {
		digest, entry, ok := s.lru.GetOldest()
		if !ok {
			return "", nil
		}
		if entry.pins == 0 {
			return digest, entry
		}
		if _, exists := s.lru.Get(digest); !exists {
			return "", nil
		}
	}
	return "", nil
}

func (s *DiskCacheStore) persistMetadataLocked(entry *diskCacheEntry) error {
	data, err := json.Marshal(entry.metadata)
	if err != nil {
		return fmt.Errorf("encode disk cache metadata: %w", err)
	}
	temp, tempPath, err := s.createCacheTemp("meta-")
	if err != nil {
		return err
	}
	defer func() {
		if err := s.removeCacheFile(tempPath); err != nil {
			s.orphanedBytes += int64(len(data))
			s.diskFillDisabled = true
			s.logger().Warn("could not remove disk cache metadata temporary file", "result", "cleanup_error")
		}
	}()
	if _, err := temp.Write(data); err != nil {
		return errors.Join(fmt.Errorf("write disk cache metadata: %w", err), wrapDiskCacheClose("close metadata temporary file", temp.Close()))
	}
	if err := temp.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync disk cache metadata: %w", err), wrapDiskCacheClose("close metadata temporary file", temp.Close()))
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close disk cache metadata temporary file: %w", err)
	}
	if err := s.rootFS.Rename(tempPath, entry.metaPath); err != nil {
		return fmt.Errorf("publish disk cache metadata: %w", err)
	}
	return nil
}

func (s *DiskCacheStore) removeEntryLocked(digest string, entry *diskCacheEntry) error {
	if entry.pins > 0 {
		entry.evictPending = true
		return nil
	}
	if err := s.removeBodyBeforeEviction(entry); err != nil {
		entry.evictPending = true
		return err
	}
	if err := s.removeMetadataAfterEviction(entry); err != nil {
		entry.evictPending = true
		return err
	}
	delete(s.entries, digest)
	s.lru.Remove(digest)
	s.bytes -= entry.metadata.Size
	if s.bytes < 0 {
		s.bytes = 0
	}
	return nil
}

func (s *DiskCacheStore) removeBodyBeforeEviction(entry *diskCacheEntry) error {
	bodyInfo, err := s.rootFS.Lstat(entry.bodyPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("inspect cached body before eviction: %w", err)
	case !bodyInfo.Mode().IsRegular():
		return errors.New("cached body is not a regular file")
	}
	if err := s.rootFS.Remove(entry.bodyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove cached body: %w", err)
	}
	return nil
}

func (s *DiskCacheStore) removeMetadataAfterEviction(entry *diskCacheEntry) error {
	metaInfo, err := s.rootFS.Lstat(entry.metaPath)
	switch {
	case err == nil && metaInfo.Mode().IsRegular():
		if removeErr := s.rootFS.Remove(entry.metaPath); removeErr != nil {
			return fmt.Errorf("remove cached metadata: %w", removeErr)
		}
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("inspect cached metadata before eviction: %w", err)
	}
	return nil
}

func (s *DiskCacheStore) createCacheTemp(prefix string) (*os.File, string, error) {
	for range 8 {
		name, err := s.newDiskCacheTempPath(prefix)
		if err != nil {
			return nil, "", err
		}
		file, err := s.rootFS.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("create disk cache temporary file: %w", err)
		}
		return file, name, nil
	}
	return nil, "", errors.New("could not allocate a unique disk cache temporary file")
}

func (s *DiskCacheStore) newDiskCacheTempPath(prefix string) (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("generate disk cache temporary name: %w", err)
	}
	return filepath.Join("temporary", prefix+hex.EncodeToString(nonce[:])), nil
}

func (s *DiskCacheStore) removeCacheFile(path string) error {
	s.rootMu.RLock()
	defer s.rootMu.RUnlock()
	if err := s.rootFS.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove disk cache file: %w", err)
	}
	return nil
}

func (s *DiskCacheStore) releaseEntry(digest string, entry *diskCacheEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.entries[digest]; current != entry {
		return
	}
	if entry.pins > 0 {
		entry.pins--
	}
	if entry.pins == 0 && entry.evictPending && !s.lockReleased {
		if err := s.removeEntryLocked(digest, entry); err != nil {
			s.logger().Warn("could not release evicted disk cache entry", "result", "cleanup_error")
		}
	}
}

func (s *DiskCacheStore) logCacheError(ctx context.Context, operation string, err error) {
	s.logger().WarnContext(ctx, "object disk cache operation failed", "operation", operation, "error", err)
	s.observe(ctx, "fill", "error", 0)
}
