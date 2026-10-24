package object

import (
	"errors"
	"fmt"

	"github.com/gofrs/flock"
)

type diskCacheClosingFill struct {
	fill *diskCacheFill
}

type diskCacheCloseSnapshot struct {
	fills   []diskCacheClosingFill
	readers []*diskCacheFileReader
	flights []*diskCacheFlightReader
}

func (s *DiskCacheStore) Close() error {
	s.closeOnce.Do(func() { s.closeErr = s.closeResources() })
	return s.closeErr
}

func (s *DiskCacheStore) closeResources() error {
	snapshot := s.markClosed()
	closeErr := closeDiskCacheDirectTracker(s.directTracker)
	closeErr = errors.Join(closeErr, closeActiveFills(s, snapshot.fills))
	closeErr = errors.Join(closeErr, closeActiveFileReaders(snapshot.readers))
	closeErr = errors.Join(closeErr, closeActiveFlightReaders(snapshot.flights))
	waitForDiskCacheFlights(snapshot.fills)
	closeErr = errors.Join(closeErr, s.closeCacheRoot())
	closeErr = errors.Join(closeErr, closeCanonicalBackend(s.backend))
	return errors.Join(closeErr, releaseCacheLock(s.lock))
}

func closeDiskCacheDirectTracker(tracker *diskCacheDirectTracker) error {
	if err := tracker.close(); err != nil {
		return fmt.Errorf("close canonical direct reads: %w", err)
	}
	return nil
}

func (s *DiskCacheStore) markClosed() diskCacheCloseSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	snapshot := diskCacheCloseSnapshot{
		fills:   make([]diskCacheClosingFill, 0, len(s.activeFlights)),
		readers: make([]*diskCacheFileReader, 0, len(s.readers)),
		flights: make([]*diskCacheFlightReader, 0, len(s.flightReaders)),
	}
	for fill := range s.activeFlights {
		fill.invalid = true
		fill.cancel()
		s.closeDiskCacheFlightReadyLocked(fill, errDiskCacheClosed)
		snapshot.fills = append(snapshot.fills, diskCacheClosingFill{fill: fill})
	}
	for reader := range s.readers {
		snapshot.readers = append(snapshot.readers, reader)
	}
	for reader := range s.flightReaders {
		snapshot.flights = append(snapshot.flights, reader)
	}
	return snapshot
}

func closeActiveFills(store *DiskCacheStore, fills []diskCacheClosingFill) error {
	var closeErr error
	for _, item := range fills {
		if err := closeActiveFill(store, item.fill); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	return closeErr
}

func closeActiveFill(store *DiskCacheStore, fill *diskCacheFill) error {
	store.mu.Lock()
	directReader := fill.directReader
	fill.directReader = nil
	store.mu.Unlock()
	var closeErr error
	if directReader != nil {
		closeErr = errors.Join(closeErr, wrapDiskCacheClose("close bypass cache reader", directReader.Close()))
	}
	fill.cancel()
	if fill.stream != nil {
		closeErr = errors.Join(closeErr, wrapDiskCacheClose("cancel active cache stream", fill.stream.CancelWithErr(errDiskCacheClosed)))
	}
	closeErr = errors.Join(closeErr, wrapDiskCacheClose("close active cache source", closeDiskCacheFlightSource(fill)))
	return closeErr
}

func closeActiveFlightReaders(readers []*diskCacheFlightReader) error {
	var closeErr error
	for _, reader := range readers {
		if err := reader.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close active shared reader: %w", err))
		}
	}
	return closeErr
}

func waitForDiskCacheFlights(fills []diskCacheClosingFill) {
	for _, item := range fills {
		<-item.fill.done
		<-item.fill.refsDone
	}
}

func closeActiveFileReaders(readers []*diskCacheFileReader) error {
	var closeErr error
	for _, reader := range readers {
		if err := reader.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close active cached reader: %w", err))
		}
	}
	return closeErr
}

func (s *DiskCacheStore) closeCacheRoot() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lockReleased = true
	if s.rootFS == nil {
		return nil
	}
	s.rootMu.Lock()
	defer s.rootMu.Unlock()
	if err := s.rootFS.Close(); err != nil {
		return fmt.Errorf("close disk cache root: %w", err)
	}
	return nil
}

func closeCanonicalBackend(backend Store) error {
	closer, ok := backend.(interface{ Close() error })
	if !ok {
		return nil
	}
	if err := closer.Close(); err != nil {
		return fmt.Errorf("close canonical object store: %w", err)
	}
	return nil
}

func releaseCacheLock(lock *flock.Flock) error {
	if lock == nil {
		return nil
	}
	if err := lock.Unlock(); err != nil {
		return fmt.Errorf("release disk cache lock: %w", err)
	}
	return nil
}
