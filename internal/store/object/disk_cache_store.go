package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
)

func (s *DiskCacheStore) Stat(ctx context.Context, digest string) (*Info, error) {
	if err := s.checkOpen(ctx); err != nil {
		return nil, err
	}
	info, err := s.backend.Stat(ctx, digest)
	if err != nil {
		return nil, fmt.Errorf("stat canonical object: %w", err)
	}
	return info, nil
}

func (s *DiskCacheStore) Exists(ctx context.Context, digest string) (bool, error) {
	if err := s.checkOpen(ctx); err != nil {
		return false, err
	}
	exists, err := s.backend.Exists(ctx, digest)
	if err != nil {
		return false, fmt.Errorf("check canonical object: %w", err)
	}
	return exists, nil
}

func (s *DiskCacheStore) Get(ctx context.Context, digest string, opts GetOptions) (io.ReadCloser, *Info, error) {
	if err := s.checkOpen(ctx); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			s.observe(ctx, "get", "canceled", 0)
		} else {
			s.observe(ctx, "get", "closed", 0)
		}
		return nil, nil, err
	}
	normalized, err := normalizeDigest(digest)
	if err != nil {
		return nil, nil, fmt.Errorf("normalize object digest: %w", err)
	}
	if reader, info, ok, err := s.openCached(ctx, normalized, opts.Range); ok || err != nil {
		return reader, info, err
	}
	s.observe(ctx, "get", "miss", 0)
	return s.getCanonical(ctx, normalized, opts)
}

func (s *DiskCacheStore) Put(ctx context.Context, digest string, reader io.Reader, opts PutOptions) (*Info, error) {
	if err := s.checkOpen(ctx); err != nil {
		return nil, err
	}
	mutation, err := s.beginKeyMutation(digest)
	if err != nil {
		return nil, fmt.Errorf("begin canonical object mutation: %w", err)
	}
	defer s.finishKeyMutation(mutation)
	info, err := s.backend.Put(ctx, digest, reader, opts)
	if err != nil {
		return info, fmt.Errorf("put canonical object: %w", err)
	}
	if evictErr := s.Evict(ctx, digest); evictErr != nil {
		s.observe(ctx, "evict", "error", 0)
		s.logger().WarnContext(ctx, "object disk cache invalidation failed after Put", "error", evictErr)
	}
	return info, nil
}

func (s *DiskCacheStore) Delete(ctx context.Context, digest string) error {
	if err := s.checkOpen(ctx); err != nil {
		return err
	}
	mutation, err := s.beginKeyMutation(digest)
	if err != nil {
		return fmt.Errorf("begin canonical object deletion: %w", err)
	}
	defer s.finishKeyMutation(mutation)
	if err := s.backend.Delete(ctx, mutation.digest); err != nil {
		s.observe(ctx, "delete", "error", 0)
		return fmt.Errorf("delete canonical object: %w", err)
	}
	s.observe(ctx, "delete", "success", 0)
	return nil
}

func (s *DiskCacheStore) WalkObjects(ctx context.Context, fn func(Info) error) error {
	if err := s.checkOpen(ctx); err != nil {
		return err
	}
	walker, ok := s.backend.(ObjectWalker)
	if !ok {
		return errors.New("canonical object store does not support walking")
	}
	if err := walker.WalkObjects(ctx, fn); err != nil {
		return fmt.Errorf("walk canonical objects: %w", err)
	}
	return nil
}

func (s *DiskCacheStore) ListObjects(ctx context.Context) ([]Info, error) {
	if err := s.checkOpen(ctx); err != nil {
		return nil, err
	}
	lister, ok := s.backend.(ObjectLister)
	if !ok {
		return nil, errors.New("canonical object store does not support listing")
	}
	objects, err := lister.ListObjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list canonical objects: %w", err)
	}
	return objects, nil
}

func (s *DiskCacheStore) Evict(ctx context.Context, digest string) error {
	if err := s.checkOpen(ctx); err != nil {
		return err
	}
	token, err := s.beginKeyMutation(digest)
	if err != nil {
		return fmt.Errorf("begin disk cache eviction: %w", err)
	}
	defer s.finishKeyMutation(token)
	if entryErr := s.entryRemovalError(token.digest); entryErr != nil {
		s.observe(ctx, "evict", "error", 0)
		return fmt.Errorf("evict disk cache entry: %w", entryErr)
	}
	s.observe(ctx, "evict", "success", 0)
	return nil
}

func (s *DiskCacheStore) entryRemovalError(digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.entries[digest]; entry != nil && entry.pins == 0 && entry.evictPending {
		return s.removeEntryLocked(digest, entry)
	}
	return nil
}

func (s *DiskCacheStore) Stats() DiskCacheStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := 0
	for _, entry := range s.entries {
		if !entry.evictPending {
			entries++
		}
	}
	return DiskCacheStats{Bytes: s.bytes + s.orphanedBytes, ReservedBytes: s.reserved, Entries: entries, Fills: len(s.fills)}
}

func (s *DiskCacheStore) checkOpen(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("object disk cache: %w", err)
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errDiskCacheClosed
	}
	return nil
}

func (s *DiskCacheStore) logger() *slog.Logger {
	if s.opts.Logger != nil {
		return s.opts.Logger
	}
	return slog.Default()
}

func (s *DiskCacheStore) observe(ctx context.Context, operation, result string, bytes int64) {
	if s.opts.Observe != nil {
		s.opts.Observe(ctx, DiskCacheEvent{Operation: operation, Result: result, Bytes: bytes})
	}
}

var _ Store = (*DiskCacheStore)(nil)
var _ ObjectWalker = (*DiskCacheStore)(nil)
var _ ObjectLister = (*DiskCacheStore)(nil)
