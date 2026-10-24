package object

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

func (s *DiskCacheStore) publishFill(ctx context.Context, fill *diskCacheFill) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.fillCanBePublishedLocked(ctx, fill) {
		return s.discardUnpublishableFillLocked(fill)
	}
	bodyPath := s.bodyPath(cacheFileStem(fill.digest))
	metaPath := s.metadataPath(cacheFileStem(fill.digest))
	return s.publishPreparedFillLocked(ctx, fill, bodyPath, metaPath)
}

func (s *DiskCacheStore) fillCanBePublishedLocked(ctx context.Context, fill *diskCacheFill) bool {
	return !s.closed && ctx.Err() == nil && !fill.invalid && s.fills[fill.digest] == fill &&
		s.keyTokenValidLocked(fill.keyToken)
}

func (s *DiskCacheStore) discardUnpublishableFillLocked(fill *diskCacheFill) error {
	if err := s.removeCacheFile(fill.tempPath); err != nil {
		return err
	}
	s.endFillLocked(fill)
	return nil
}

func (s *DiskCacheStore) publishPreparedFillLocked(
	ctx context.Context,
	fill *diskCacheFill,
	bodyPath, metaPath string,
) error {
	if err := s.rootFS.Rename(fill.tempPath, bodyPath); err != nil {
		return fmt.Errorf("publish cached body: %w", err)
	}
	bodyInfo, err := s.rootFS.Stat(bodyPath)
	if err != nil {
		if removeErr := s.removePublishedBodyLocked(fill, bodyPath, metaPath); removeErr != nil {
			err = errors.Join(err, removeErr)
		}
		s.endFillLocked(fill)
		return fmt.Errorf("stat published cached body: %w", err)
	}
	entry := s.newPublishedEntry(fill, bodyPath, metaPath, bodyInfo.ModTime())
	if err := s.persistMetadataLocked(entry); err != nil {
		publishErr := errors.Join(err, s.removePublishedBodyLocked(fill, bodyPath, metaPath))
		s.endFillLocked(fill)
		return fmt.Errorf("publish cached metadata: %w", publishErr)
	}
	return s.registerPublishedFillLocked(ctx, fill, entry, bodyPath, metaPath)
}

func (s *DiskCacheStore) registerPublishedFillLocked(
	ctx context.Context,
	fill *diskCacheFill,
	entry *diskCacheEntry,
	bodyPath, metaPath string,
) error {
	if s.fills[fill.digest] != fill || fill.invalid || s.closed {
		if err := removeDiskCachePair(s.rootFS, entry.metaPath, entry.bodyPath); err != nil {
			s.trackUnpublishedEntryLocked(fill, bodyPath, metaPath)
			s.logger().Warn("could not remove invalid published cache entry", "result", "cleanup_error")
		}
		s.endFillLocked(fill)
		return nil
	}
	s.entries[fill.digest] = entry
	s.lru.Add(fill.digest, entry)
	s.bytes += entry.metadata.Size
	s.endFillLocked(fill)
	s.observe(ctx, "fill", "success", entry.metadata.Size)
	return nil
}

func (s *DiskCacheStore) removePublishedBodyLocked(fill *diskCacheFill, bodyPath, metaPath string) error {
	if err := s.rootFS.Remove(bodyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.trackUnpublishedEntryLocked(fill, bodyPath, metaPath)
		return fmt.Errorf("remove untracked cached body: %w", err)
	}
	return nil
}

func (s *DiskCacheStore) newPublishedEntry(
	fill *diskCacheFill,
	bodyPath, metaPath string,
	modTime time.Time,
) *diskCacheEntry {
	now := time.Now()
	metadata := diskCacheMetadata{
		Digest:      fill.digest,
		Size:        fill.reserve,
		ContentType: fill.info.ContentType,
		ETag:        fill.info.ETag,
		StoredAt:    now.UnixNano(),
		LastAccess:  now.UnixNano(),
		FileModTime: modTime.UnixNano(),
	}
	return &diskCacheEntry{metadata: metadata, bodyPath: bodyPath, metaPath: metaPath, lastAccess: now}
}

func (s *DiskCacheStore) trackUnpublishedEntryLocked(fill *diskCacheFill, bodyPath, metaPath string) {
	if s.entries[fill.digest] != nil {
		return
	}
	entry := &diskCacheEntry{
		metadata:     diskCacheMetadata{Digest: fill.digest, Size: fill.reserve},
		bodyPath:     bodyPath,
		metaPath:     metaPath,
		evictPending: true,
	}
	s.entries[fill.digest] = entry
	s.lru.Add(fill.digest, entry)
	s.bytes += fill.reserve
}

func (s *DiskCacheStore) endFillLocked(fill *diskCacheFill) {
	if s.fills[fill.digest] != fill {
		return
	}
	delete(s.fills, fill.digest)
	s.reserved -= fill.reserve
	if s.reserved < 0 {
		s.reserved = 0
	}
	s.releaseKeyTokenLocked(fill.keyToken)
}
