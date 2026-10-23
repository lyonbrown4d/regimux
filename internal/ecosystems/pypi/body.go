package pypi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/spool"
)

type packageBody struct {
	io.ReadCloser
	ctx          context.Context
	service      *Service
	req          Request
	route        Route
	fetched      *upstreamFetch
	tmp          *spool.TempBody
	hash         hash.Hash
	size         int64
	expectedSize int64
	sizeKnown    bool
	complete     bool
	failed       bool
	finish       func()
	cacheSlot    bool
	once         sync.Once
	mu           sync.Mutex
	stopCleanup  func() bool
	started      time.Time
	readLease    *time.Timer
	readDeadline time.Time
	closed       bool
	abandoned    bool
}

func (s *Service) packageCacheBody(ctx context.Context, req Request, route Route, fetched *upstreamFetch, finish func()) io.ReadCloser {
	b := &packageBody{ReadCloser: fetched.body, ctx: ctx, service: s, req: req, route: route, fetched: fetched, hash: sha256.New(), finish: finish}
	if expectedSize, err := strconv.ParseInt(strings.TrimSpace(fetched.headers.Get("Content-Length")), 10, 64); err == nil && expectedSize >= 0 {
		b.expectedSize = expectedSize
		b.sizeKnown = true
		if expectedSize == 0 {
			b.complete = true
			b.validateDigest()
		}
	}
	b.started = time.Now()
	// Temp-file failures degrade caching, while the upstream remains readable.
	select {
	case s.cacheSlots <- struct{}{}:
		b.cacheSlot = true
		tmp, err := spool.NewTempBody("regimux-pypi-stream-*")
		if err == nil {
			b.tmp = tmp
		}
	default:
	}
	b.mu.Lock()
	//nolint:contextcheck // A bounded upstream transfer context owns this read lifecycle when present.
	cleanupCtx := fetched.transferContext
	if cleanupCtx == nil {
		cleanupCtx = ctx
	}
	b.stopCleanup = context.AfterFunc(cleanupCtx, func() {
		if err := b.close(ctx); err != nil {
			s.logger.DebugContext(ctx, "close canceled pypi body failed")
		}
	})
	b.armReadLease()
	b.mu.Unlock()
	return b
}

func (b *packageBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.abandoned {
		return 0, io.ErrClosedPipe
	}
	b.pauseReadLease()
	defer b.armReadLease()
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		if readErr := b.validateRead(p[:n]); readErr != nil {
			return n, readErr
		}
	}
	if err == io.EOF {
		return b.finishAtEOF(n)
	} else if err != nil {
		b.failed = true
	}
	return n, err //nolint:wrapcheck // io.Reader must preserve the EOF sentinel.
}

func (b *packageBody) validateRead(p []byte) error {
	b.recordBytes(p)
	if b.sizeKnown && b.size > b.expectedSize {
		b.failed = true
		return errors.New("pypi package body exceeds Content-Length")
	}
	if b.sizeKnown && b.size == b.expectedSize {
		b.complete = true
		if !b.validateDigest() {
			return errors.New("pypi package SHA-256 mismatch")
		}
	}
	return nil
}

func (b *packageBody) finishAtEOF(n int) (int, error) {
	if b.sizeKnown && b.size != b.expectedSize {
		b.failed = true
		return n, io.ErrUnexpectedEOF
	}
	b.complete = true
	if !b.validateDigest() {
		return n, errors.New("pypi package SHA-256 mismatch")
	}
	return n, io.EOF
}

func (b *packageBody) validateDigest() bool {
	if validSHA256(b.req.SHA256) && !strings.EqualFold(hex.EncodeToString(b.hash.Sum(nil)), b.req.SHA256) {
		b.failed = true
		return false
	}
	return true
}

func (b *packageBody) recordBytes(p []byte) {
	b.size += int64(len(p))
	if _, err := b.hash.Write(p); err != nil {
		b.failed = true
	}
	if b.tmp != nil && !b.failed {
		if _, err := b.tmp.Write(p); err != nil {
			b.failed = true
		}
	}
}

func (b *packageBody) Close() error { return b.close(b.ctx) }

func (b *packageBody) close(ctx context.Context) error {
	var closeErr error
	b.once.Do(func() {
		closeErr = b.ReadCloser.Close()
		b.mu.Lock()
		defer b.mu.Unlock()
		b.closed = true
		b.pauseReadLease()
		defer b.observeTransfer(ctx)
		if b.stopCleanup != nil {
			b.stopCleanup()
		}
		if b.cacheSlot {
			defer func() { <-b.service.cacheSlots }()
		}
		if b.finish != nil {
			defer b.finish()
		}
		if b.tmp == nil {
			return
		}
		defer closeReadCloser(b.tmp, b.service.logger, "close pypi spool")
		b.cacheComplete(ctx)
	})
	return wrapError(closeErr, "close pypi package body")
}

func (b *packageBody) cacheComplete(ctx context.Context) {
	if !b.complete || b.failed || ctx.Err() != nil {
		return
	}
	if err := artifactcache.ValidateBody(b.tmp, b.size, b.fetched.headers, nil); err != nil {
		return
	}
	if _, err := b.tmp.Seek(0, io.SeekStart); err != nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, pypiCacheWriteTimeout)
	defer cancel()
	entry, err := b.service.cache.Put(writeCtx, artifactcache.PutRequest{Key: artifactKey(b.route), AcceptKey: acceptKeyPyPI, Body: b.tmp, Headers: b.fetched.headers, ContentType: routeContentType(b.route, b.fetched.headers)})
	if entry.Body != nil {
		closeReadCloser(entry.Body, b.service.logger, "close cached pypi copy")
	}
	if err != nil {
		b.service.logger.WarnContext(ctx, "pypi package cache write skipped", "alias", b.route.Alias)
		b.service.observePackageCache(ctx, b.route.Alias, "write_error")
		return
	}
	b.service.publishPackageEvent(ctx, events.CacheStore{Kind: "pypi.package", Alias: ecosystem.ScopedAlias(ecosystem.PyPI, b.route.Alias), Digest: entry.Digest, Size: entry.Size})
}

func (b *packageBody) observeTransfer(ctx context.Context) {
	reason := ""
	if !b.complete || b.failed {
		reason = "incomplete"
	}
	if ctx.Err() != nil {
		reason = "canceled"
	}
	b.service.publishPackageEvent(ctx, events.UpstreamRequest{Alias: ecosystem.ScopedAlias(ecosystem.PyPI, b.route.Alias), Operation: "pypi.package.body", Attempts: 1, Duration: time.Since(b.started), Size: b.size, Error: reason})
}
