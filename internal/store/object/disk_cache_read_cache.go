package object

import (
	"context"
	"os"
	"time"
)

func (s *DiskCacheStore) cachedEntryFreshLocked(ctx context.Context, digest string, entry *diskCacheEntry) (bool, string) {
	if entry.evictPending {
		return false, ""
	}
	if _, ok := s.lru.Get(digest); !ok {
		return false, ""
	}
	if time.Since(entry.lastAccess) > s.opts.TTL {
		s.markEntryExpiredLocked(ctx, digest, entry)
		return false, "expired"
	}
	return true, ""
}

func (s *DiskCacheStore) openVerifiedCacheBodyLocked(
	ctx context.Context,
	digest string,
	entry *diskCacheEntry,
) (*os.File, string) {
	pathInfo, err := s.rootFS.Lstat(entry.bodyPath)
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
		s.markEntryExpiredLocked(ctx, digest, entry)
		return nil, "corrupt"
	}
	file, err := s.rootFS.Open(entry.bodyPath)
	if err != nil {
		s.markEntryExpiredLocked(ctx, digest, entry)
		return nil, "corrupt"
	}
	fileInfo, err := file.Stat()
	if err == nil && fileInfo.Mode().IsRegular() && fileInfo.Size() == entry.metadata.Size &&
		fileInfo.ModTime().UnixNano() == entry.metadata.FileModTime {
		return file, ""
	}
	discardCachedFileClose(ctx, s, file, "validate")
	s.markEntryExpiredLocked(ctx, digest, entry)
	return nil, "corrupt"
}

func (s *DiskCacheStore) markEntryExpiredLocked(ctx context.Context, digest string, entry *diskCacheEntry) {
	entry.evictPending = true
	if entry.pins == 0 {
		if err := s.removeEntryLocked(digest, entry); err != nil {
			s.logger().WarnContext(ctx, "could not remove invalid cached object", "result", "cleanup_error")
		}
	}
}
