package object

import (
	"errors"
	"fmt"
	"io"

	streamlib "github.com/djherbis/stream"
)

func (s *DiskCacheStore) runDiskCacheFlight(fill *diskCacheFill, opts GetOptions) {
	defer close(fill.done)
	reader, info, err := s.openDiskCacheFlight(fill, opts)
	if err != nil {
		s.finishDiskCacheFlightOpenError(fill, reader, err)
		return
	}
	if info == nil || info.Size > s.opts.MaxObjectBytes || info.Size > s.opts.MaxBytes {
		s.finishDiskCacheFlightBypass(fill, reader, info)
		return
	}
	if !s.prepareDiskCacheFlightOrBypass(fill, reader, *info) {
		return
	}
	s.copyDiskCacheFlight(fill)
}

func (s *DiskCacheStore) openDiskCacheFlight(
	fill *diskCacheFill,
	opts GetOptions,
) (io.ReadCloser, *Info, error) {
	reader, info, err := s.backend.Get(fill.ctx, fill.digest, opts)
	if err != nil {
		return reader, info, fmt.Errorf("get canonical object: %w", err)
	}
	if err := s.canonicalOpenError(fill.ctx); err != nil {
		return reader, info, err
	}
	if reader == nil {
		return nil, nil, errors.New("canonical object store returned a nil reader")
	}
	if err := validateCanonicalFlightInfo(fill.digest, info); err != nil {
		return reader, info, err
	}
	return reader, info, nil
}

func (s *DiskCacheStore) prepareDiskCacheFlightOrBypass(fill *diskCacheFill, reader io.ReadCloser, info Info) bool {
	prepareErr := s.prepareDiskCacheFlight(fill, reader, info)
	switch {
	case prepareErr == nil:
		return true
	case errors.Is(prepareErr, errDiskCacheFillUnavailable):
		s.finishDiskCacheFlightBypass(fill, reader, &info)
	case s.canonicalOpenError(fill.ctx) != nil:
		s.finishDiskCacheFlightOpenError(fill, reader, prepareErr)
	default:
		s.finishDiskCacheFlightBypass(fill, reader, &info)
		s.logCacheError(fill.ctx, "create_fill", prepareErr)
	}
	return false
}

func validateCanonicalFlightInfo(digest string, info *Info) error {
	if info == nil {
		return nil
	}
	canonicalDigest, err := normalizeDigest(info.Digest)
	if err != nil || canonicalDigest != digest || info.Size < 0 {
		return ErrDigestMismatch
	}
	return nil
}

func (s *DiskCacheStore) prepareDiskCacheFlight(fill *diskCacheFill, source io.ReadCloser, info Info) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.diskCacheFlightValidLocked(fill) || fill.refs == 0 {
		return errDiskCacheClosed
	}
	if !s.makeRoomForExistingFillLocked(info.Size) {
		return errDiskCacheFillUnavailable
	}
	path, err := s.newDiskCacheTempPath("fill-")
	if err != nil {
		return fmt.Errorf("create disk cache fill: %w", err)
	}
	fs := &diskCacheStreamFS{store: s, name: path}
	stream, err := streamlib.NewStream(path, fs)
	if err != nil {
		return errors.Join(fmt.Errorf("create disk cache stream: %w", err), removeFailedStreamCreate(s, fs))
	}
	if err := stream.SetSeekEnd(info.Size); err != nil {
		return errors.Join(fmt.Errorf("set disk cache stream size: %w", err), stream.CancelWithErr(err), s.removeCacheFile(path))
	}
	fill.info = info
	fill.tempPath = path
	fill.reserve = info.Size
	fill.stream = stream
	fill.source = source
	s.reserved += info.Size
	s.closeDiskCacheFlightReadyLocked(fill, nil)
	return nil
}

func (s *DiskCacheStore) diskCacheFlightValidLocked(fill *diskCacheFill) bool {
	return !s.closed && !fill.invalid && s.fills[fill.digest] == fill && s.keyTokenValidLocked(fill.keyToken)
}

func (s *DiskCacheStore) closeDiskCacheFlightReadyLocked(fill *diskCacheFill, err error) {
	if fill.readyClosed {
		return
	}
	fill.readyErr = err
	fill.readyClosed = true
	close(fill.ready)
}

func (s *DiskCacheStore) finishDiskCacheFlightOpenError(fill *diskCacheFill, reader io.ReadCloser, err error) {
	err = errors.Join(err, readerCloseError(reader))
	s.mu.Lock()
	fill.producerErr = err
	fill.producerDone = true
	fill.bypass = true
	s.closeDiskCacheFlightReadyLocked(fill, fmt.Errorf("get canonical object: %w", err))
	s.endFillLocked(fill)
	s.cleanupDiskCacheFlightLifetimeLocked(fill)
	s.mu.Unlock()
	s.stopDiskCacheFlightContext(fill)
	s.observe(fill.ctx, "get", "error", 0)
}

func (s *DiskCacheStore) finishDiskCacheFlightBypass(fill *diskCacheFill, reader io.ReadCloser, info *Info) {
	keepReader := s.assignDiskCacheFlightBypass(fill, reader, info)
	if !keepReader {
		if err := reader.Close(); err != nil {
			s.logCacheError(fill.ctx, "close_uncached_fill", err)
		}
		s.stopDiskCacheFlightContext(fill)
	}
	s.mu.Lock()
	s.cleanupDiskCacheFlightLifetimeLocked(fill)
	s.mu.Unlock()
	s.observeDiskCacheFlightBypass(fill, info)
}

func (s *DiskCacheStore) assignDiskCacheFlightBypass(fill *diskCacheFill, reader io.ReadCloser, info *Info) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	fill.producerDone = true
	fill.bypass = true
	fill.directInfo = info
	fill.readyErr = errDiskCacheFillUnavailable
	if s.closed {
		fill.readyErr = errDiskCacheClosed
	}
	if fill.ownerActive && fill.refs > 0 && !s.closed {
		fill.directReader = &storeBoundReadCloser{reader: reader, cancel: fill.cancel}
		reader = nil
	} else {
		fill.ownerActive = false
	}
	s.closeDiskCacheFlightReadyLocked(fill, fill.readyErr)
	s.endFillLocked(fill)
	return reader == nil
}

func (s *DiskCacheStore) observeDiskCacheFlightBypass(fill *diskCacheFill, info *Info) {
	if info != nil && (info.Size > s.opts.MaxObjectBytes || info.Size > s.opts.MaxBytes) {
		s.observe(fill.ctx, "bypass", "oversize", info.Size)
	} else {
		s.observe(fill.ctx, "bypass", "capacity", 0)
	}
}

func (s *DiskCacheStore) stopDiskCacheFlightContext(fill *diskCacheFill) {
	fill.cancel()
}

func (s *DiskCacheStore) copyDiskCacheFlight(fill *diskCacheFill) {
	verifier, err := newDiskCacheDigestVerifier(fill.digest)
	if err == nil {
		err = copyVerifiedDiskCacheStream(fill, verifier)
	}
	closeErr := closeDiskCacheFlightSource(fill)
	if err == nil && closeErr != nil {
		s.logCacheError(fill.ctx, "close_canonical", closeErr)
	}
	if err == nil {
		err = fill.stream.Close()
	}
	if err != nil {
		cause := err
		err = errors.Join(cause, fill.stream.CancelWithErr(cause))
	}
	s.mu.Lock()
	fill.producerErr = err
	fill.producerDone = true
	s.mu.Unlock()
	if err != nil {
		s.invalidateDiskCacheFlightReaders(fill)
	}
	s.maybeFinishDiskCacheFlight(fill)
}

func closeDiskCacheFlightSource(fill *diskCacheFill) error {
	var closeErr error
	fill.sourceCloseOnce.Do(func() {
		if fill.source != nil {
			closeErr = fill.source.Close()
			if closeErr != nil {
				closeErr = fmt.Errorf("close canonical object reader: %w", closeErr)
			}
		}
		fill.sourceCloseErr = closeErr
	})
	return fill.sourceCloseErr
}

func removeFailedStreamCreate(store *DiskCacheStore, fs *diskCacheStreamFS) error {
	if !fs.created {
		return nil
	}
	return store.removeCacheFile(fs.name)
}
