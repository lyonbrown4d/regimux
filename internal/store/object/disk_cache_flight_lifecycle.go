package object

import (
	"context"
	"errors"
	"fmt"
	"io"
)

func (s *DiskCacheStore) invalidateDiskCacheFlightReaders(fill *diskCacheFill) {
	s.mu.Lock()
	readers := make([]*diskCacheFlightReader, 0, fill.refs)
	for reader := range s.flightReaders {
		if reader.fill == fill {
			readers = append(readers, reader)
		}
	}
	s.mu.Unlock()
	for _, reader := range readers {
		if err := reader.Close(); err != nil {
			s.logCacheError(fill.ctx, "close_failed_flight_reader", err)
		}
	}
}

func (s *DiskCacheStore) maybeFinishDiskCacheFlight(fill *diskCacheFill) {
	s.mu.Lock()
	if fill.finalizing || !fill.producerDone || fill.refs > 0 {
		s.mu.Unlock()
		return
	}
	fill.finalizing = true
	shouldPublish := fill.producerErr == nil && fill.fullConsumed && s.diskCacheFlightValidLocked(fill)
	s.mu.Unlock()
	switch {
	case shouldPublish:
		if err := s.publishFill(fill.ctx, fill); err != nil {
			s.logCacheError(fill.ctx, "publish_fill", err)
			s.abortFill(fill)
		}
	case fill.tempPath != "":
		s.abortFill(fill)
	default:
		s.mu.Lock()
		s.endFillLocked(fill)
		s.cleanupDiskCacheFlightLifetimeLocked(fill)
		s.mu.Unlock()
	}
	s.stopDiskCacheFlightContext(fill)
	s.mu.Lock()
	s.cleanupDiskCacheFlightLifetimeLocked(fill)
	s.mu.Unlock()
}

func (s *DiskCacheStore) releaseDiskCacheFlightRef(fill *diskCacheFill, leader, fullyRead bool) bool {
	state := s.recordDiskCacheFlightRelease(fill, leader, fullyRead)
	s.finishDiskCacheFlightRelease(fill, state)
	return state.waitForProducer
}

type diskCacheFlightRelease struct {
	reader          io.ReadCloser
	cancel          bool
	waitForProducer bool
	last            bool
	producerDone    bool
	bypass          bool
}

func (s *DiskCacheStore) recordDiskCacheFlightRelease(
	fill *diskCacheFill,
	leader, fullyRead bool,
) diskCacheFlightRelease {
	s.mu.Lock()
	defer s.mu.Unlock()
	updateDiskCacheFlightRefLocked(fill, leader, fullyRead)
	return s.captureDiskCacheFlightReleaseLocked(fill, leader)
}

func updateDiskCacheFlightRefLocked(fill *diskCacheFill, leader, fullyRead bool) {
	if fill.refs > 0 {
		fill.refs--
	}
	if fill.refs == 0 {
		fill.refsDoneOnce.Do(func() { close(fill.refsDone) })
	}
	if leader {
		fill.ownerActive = false
	}
	if fullyRead {
		fill.fullConsumed = true
	}
}

func (s *DiskCacheStore) captureDiskCacheFlightReleaseLocked(
	fill *diskCacheFill,
	leader bool,
) diskCacheFlightRelease {
	last := fill.refs == 0
	producerDone := fill.producerDone
	cancelProducer := last && !producerDone && !fill.fullConsumed
	if cancelProducer {
		fill.invalid = true
	}
	return diskCacheFlightRelease{
		reader: takeDiskCacheFlightDirectReaderLocked(fill, last, leader),
		cancel: cancelProducer, waitForProducer: last && !producerDone,
		last: last, producerDone: producerDone, bypass: fill.bypass,
	}
}

func takeDiskCacheFlightDirectReaderLocked(fill *diskCacheFill, last, leader bool) io.ReadCloser {
	if !last && !leader {
		return nil
	}
	reader := fill.directReader
	fill.directReader = nil
	return reader
}

func (s *DiskCacheStore) finishDiskCacheFlightRelease(fill *diskCacheFill, state diskCacheFlightRelease) {
	closeErr := closeReleasedDiskCacheFlightReader(s, fill, state.reader)
	if state.cancel {
		closeErr = errors.Join(closeErr, cancelDiskCacheFlight(fill))
	}
	if closeErr != nil {
		s.logCacheError(fill.ctx, "release_shared_flight", closeErr)
	}
	s.finishReleasedDiskCacheFlight(fill, state)
}

func closeReleasedDiskCacheFlightReader(
	store *DiskCacheStore,
	fill *diskCacheFill,
	reader io.ReadCloser,
) error {
	if reader == nil {
		return nil
	}
	store.stopDiskCacheFlightContext(fill)
	if err := reader.Close(); err != nil {
		return fmt.Errorf("close bypass reader: %w", err)
	}
	return nil
}

func cancelDiskCacheFlight(fill *diskCacheFill) error {
	fill.cancel()
	closeErr := closeDiskCacheFlightSource(fill)
	if fill.stream != nil {
		closeErr = errors.Join(closeErr, fill.stream.CancelWithErr(context.Canceled))
	}
	return closeErr
}

func (s *DiskCacheStore) finishReleasedDiskCacheFlight(
	fill *diskCacheFill,
	state diskCacheFlightRelease,
) {
	switch {
	case state.last && state.producerDone && state.bypass:
		s.stopDiskCacheFlightContext(fill)
	case state.producerDone && !state.bypass:
		s.maybeFinishDiskCacheFlight(fill)
	case state.producerDone:
		s.mu.Lock()
		s.cleanupDiskCacheFlightLifetimeLocked(fill)
		s.mu.Unlock()
	}
}

func (s *DiskCacheStore) cleanupDiskCacheFlightLifetimeLocked(fill *diskCacheFill) {
	if fill.refs == 0 && fill.producerDone {
		delete(s.activeFlights, fill)
	}
}
