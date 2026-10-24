package object

import (
	"context"
	"errors"
	"fmt"
	"io"
)

func (s *DiskCacheStore) getCanonicalSharedFull(
	ctx context.Context,
	digest string,
	opts GetOptions,
) (io.ReadCloser, *Info, error) {
	fill, leader, shared, err := s.joinDiskCacheFlight(ctx, digest)
	if err != nil {
		return nil, nil, err
	}
	if !shared {
		if reader, info, ok, openErr := s.openCached(ctx, digest, nil); ok || openErr != nil {
			return reader, info, openErr
		}
		s.observe(ctx, "bypass", "capacity", 0)
		return s.getCanonicalDirect(ctx, digest, opts)
	}
	if leader {
		go s.runDiskCacheFlight(fill, opts)
	}
	reader, info, err := s.awaitDiskCacheFlight(ctx, fill, leader)
	if errors.Is(err, errDiskCacheFillUnavailable) && !leader {
		if openErr := s.checkOpen(ctx); openErr != nil {
			return nil, nil, openErr
		}
		s.observe(ctx, "bypass", "capacity", 0)
		return s.getCanonicalDirect(ctx, digest, opts)
	}
	return reader, info, err
}

func (s *DiskCacheStore) joinDiskCacheFlight(
	ctx context.Context,
	digest string,
) (*diskCacheFill, bool, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, false, fmt.Errorf("open canonical object: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, false, false, errDiskCacheClosed
	}
	if fill := s.fills[digest]; fill != nil {
		if diskCacheFlightAcceptsReader(fill) {
			fill.refs++
			return fill, false, true, nil
		}
		return nil, false, false, nil
	}
	if s.entries[digest] != nil || s.diskFillDisabled || len(s.fills) >= s.opts.MaxConcurrentFills {
		return nil, false, false, nil
	}
	token := s.acquireKeyTokenLocked(digest)
	sharedCtx, cancel := newDiskCacheSharedContext(ctx)
	fill := &diskCacheFill{
		digest:      digest,
		keyToken:    token,
		ctx:         sharedCtx,
		cancel:      cancel,
		ready:       make(chan struct{}),
		done:        make(chan struct{}),
		refsDone:    make(chan struct{}),
		refs:        1,
		ownerActive: true,
	}
	s.fills[digest] = fill
	s.activeFlights[fill] = struct{}{}
	return fill, true, true, nil
}

func diskCacheFlightAcceptsReader(fill *diskCacheFill) bool {
	return !fill.invalid && !fill.finalizing && fill.refs > 0 && fill.readyErr == nil
}

func newDiskCacheSharedContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(context.WithoutCancel(parent))
}

func (s *DiskCacheStore) awaitDiskCacheFlight(
	ctx context.Context,
	fill *diskCacheFill,
	leader bool,
) (io.ReadCloser, *Info, error) {
	if err := waitForDiskCacheFlightReady(ctx, fill); err != nil {
		s.releaseDiskCacheFlightRef(fill, leader, false)
		return nil, nil, err
	}
	reader, info, direct, err := s.takeDiskCacheFlightReader(fill, leader)
	if err != nil {
		s.releaseDiskCacheFlightRef(fill, leader, false)
		return nil, nil, err
	}
	return s.registerDiskCacheFlightReader(ctx, fill, reader, info, direct)
}

func waitForDiskCacheFlightReady(ctx context.Context, fill *diskCacheFill) error {
	select {
	case <-fill.ready:
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("wait for shared canonical object: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for shared canonical object: %w", ctx.Err())
	}
}

func (s *DiskCacheStore) takeDiskCacheFlightReader(
	fill *diskCacheFill,
	leader bool,
) (io.ReadCloser, *Info, bool, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, nil, false, errDiskCacheClosed
	}
	readyErr := fill.readyErr
	if readyErr != nil {
		reader, info, direct, unused, readErr := s.takeUnavailableDiskCacheFlightReaderLocked(fill, leader, readyErr)
		s.mu.Unlock()
		if unused != nil {
			readErr = errors.Join(readErr, readerCloseError(unused))
		}
		return reader, info, direct, readErr
	}
	stream := fill.stream
	info := fill.info
	s.mu.Unlock()
	reader, err := stream.NextReader()
	if err != nil {
		return nil, nil, false, fmt.Errorf("open shared canonical stream reader: %w", err)
	}
	return reader, &info, false, nil
}

func (s *DiskCacheStore) takeUnavailableDiskCacheFlightReaderLocked(
	fill *diskCacheFill,
	leader bool,
	readyErr error,
) (io.ReadCloser, *Info, bool, io.ReadCloser, error) {
	if errors.Is(readyErr, errDiskCacheFillUnavailable) && leader && fill.ownerActive && fill.directReader != nil {
		reader := fill.directReader
		fill.directReader = nil
		fill.ownerActive = false
		return reader, fill.directInfo, true, nil, nil
	}
	if !errors.Is(readyErr, errDiskCacheFillUnavailable) || !leader || fill.directReader == nil {
		return nil, nil, false, nil, readyErr
	}
	reader := fill.directReader
	fill.directReader = nil
	fill.ownerActive = false
	return nil, nil, false, reader, readyErr
}

func (s *DiskCacheStore) registerDiskCacheFlightReader(
	ctx context.Context,
	fill *diskCacheFill,
	reader io.ReadCloser,
	info *Info,
	direct bool,
) (io.ReadCloser, *Info, error) {
	flightReader := &diskCacheFlightReader{store: s, fill: fill, callerCtx: ctx, source: reader, directReader: direct}
	flightReader.mu.Lock()
	flightReader.stopCaller = context.AfterFunc(ctx, func() {
		if err := flightReader.closeWithoutWaiting(); err != nil {
			s.logCacheError(ctx, "close_canceled_flight_reader", err)
		}
	})
	flightReader.mu.Unlock()
	s.mu.Lock()
	flightReader.mu.Lock()
	if s.closed || ctx.Err() != nil || flightReader.closed || fill.refs <= 0 || (!direct && fill.producerErr != nil) {
		getErr := fill.producerErr
		callerErr := ctx.Err()
		flightReader.mu.Unlock()
		s.mu.Unlock()
		closeErr := flightReader.closeWithoutWaiting()
		if callerErr != nil {
			getErr = callerErr
		} else if getErr == nil {
			getErr = errDiskCacheClosed
		}
		return nil, nil, errors.Join(getErr, closeErr)
	}
	s.flightReaders[flightReader] = struct{}{}
	flightReader.mu.Unlock()
	s.mu.Unlock()
	return flightReader, info, nil
}

func (s *DiskCacheStore) getCanonicalDirect(ctx context.Context, digest string, opts GetOptions) (io.ReadCloser, *Info, error) {
	return s.directTracker.open(ctx, s.backend, digest, opts)
}
