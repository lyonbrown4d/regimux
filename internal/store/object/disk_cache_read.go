package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	ocidigest "github.com/opencontainers/go-digest"
)

func (s *DiskCacheStore) openCached(
	ctx context.Context,
	digest string,
	requested *HTTPRange,
) (io.ReadCloser, *Info, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, true, fmt.Errorf("open cached object: %w", err)
	}
	file, entry, hit := s.openPinnedCachedFile(ctx, digest)
	if !hit {
		return nil, nil, false, nil
	}
	return s.finishCachedOpen(ctx, digest, requested, file, entry)
}

func (s *DiskCacheStore) finishCachedOpen(
	ctx context.Context,
	digest string,
	requested *HTTPRange,
	file *os.File,
	entry *diskCacheEntry,
) (io.ReadCloser, *Info, bool, error) {
	metadata := entry.metadata
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		closeErr := closeCachedFile(ctx, s, file, "seek")
		s.releaseEntry(digest, entry)
		return nil, nil, true, fmt.Errorf("seek cached object %q: %w", digest, errors.Join(err, closeErr))
	}
	resultInfo := Info{
		Digest:      metadata.Digest,
		Size:        metadata.Size,
		ContentType: metadata.ContentType,
		ETag:        metadata.ETag,
		Path:        filepath.Join(s.root, entry.bodyPath),
	}
	var body io.Reader = file
	if requested != nil {
		resolved, rangeErr := requested.Resolve(metadata.Size)
		if rangeErr != nil {
			closeErr := closeCachedFile(ctx, s, file, "range")
			s.releaseEntry(digest, entry)
			return nil, nil, true, errors.Join(fmt.Errorf("resolve cached object range: %w", rangeErr), closeErr)
		}
		if _, err := file.Seek(resolved.Start, io.SeekStart); err != nil {
			closeErr := closeCachedFile(ctx, s, file, "range")
			s.releaseEntry(digest, entry)
			return nil, nil, true, fmt.Errorf("seek cached range %q: %w", digest, errors.Join(err, closeErr))
		}
		resultInfo.Size = resolved.Length()
		body = io.LimitReader(file, resolved.Length())
	}
	s.observe(ctx, "get", "hit", resultInfo.Size)
	reader := &diskCacheFileReader{ctx: ctx, body: body, file: file, store: s, digest: digest, entry: entry}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		closeErr := closeCachedFile(ctx, s, file, "closed")
		s.releaseEntry(digest, entry)
		return nil, nil, true, errors.Join(errDiskCacheClosed, closeErr)
	}
	s.readers[reader] = struct{}{}
	s.mu.Unlock()
	return reader, &resultInfo, true, nil
}

func (s *DiskCacheStore) openPinnedCachedFile(ctx context.Context, digest string) (*os.File, *diskCacheEntry, bool) {
	s.mu.Lock()
	file, entry, result := s.openPinnedCachedFileLocked(ctx, digest)
	s.mu.Unlock()
	if file == nil {
		if result != "" {
			s.observe(ctx, "get", result, 0)
		}
		return nil, nil, false
	}
	return file, entry, true
}

func (s *DiskCacheStore) openPinnedCachedFileLocked(
	ctx context.Context,
	digest string,
) (*os.File, *diskCacheEntry, string) {
	if s.closed {
		return nil, nil, ""
	}
	entry := s.entries[digest]
	if entry == nil {
		return nil, nil, ""
	}
	fresh, result := s.cachedEntryFreshLocked(ctx, digest, entry)
	if !fresh {
		return nil, nil, result
	}
	file, result := s.openVerifiedCacheBodyLocked(ctx, digest, entry)
	if file != nil {
		entry.pins++
		entry.lastAccess = time.Now()
	}
	return file, entry, result
}

func newDiskCacheDigestVerifier(digest string) (ocidigest.Verifier, error) {
	parsed, err := ocidigest.Parse(digest)
	if err != nil {
		return nil, fmt.Errorf("parse object digest: %w", err)
	}
	return parsed.Verifier(), nil
}

func (s *DiskCacheStore) getCanonical(
	ctx context.Context,
	digest string,
	opts GetOptions,
) (io.ReadCloser, *Info, error) {
	if opts.Range != nil {
		return s.getCanonicalRange(ctx, digest, opts)
	}
	return s.getCanonicalFull(ctx, digest, opts)
}

func (s *DiskCacheStore) getCanonicalRange(ctx context.Context, digest string, opts GetOptions) (io.ReadCloser, *Info, error) {
	return s.directTracker.open(ctx, s.backend, digest, opts)
}

func (s *DiskCacheStore) getCanonicalFull(ctx context.Context, digest string, opts GetOptions) (io.ReadCloser, *Info, error) {
	return s.getCanonicalSharedFull(ctx, digest, opts)
}
