package object

func (s *DiskCacheStore) abortFill(fill *diskCacheFill) {
	cleanupErr := s.removeCacheFile(fill.tempPath)
	s.mu.Lock()
	if s.fills[fill.digest] == fill {
		delete(s.fills, fill.digest)
		s.reserved -= fill.reserve
		if s.reserved < 0 {
			s.reserved = 0
		}
		if cleanupErr != nil {
			s.orphanedBytes += fill.reserve
			s.diskFillDisabled = true
		}
		s.releaseKeyTokenLocked(fill.keyToken)
	}
	s.mu.Unlock()
	if cleanupErr != nil {
		s.logger().Warn("could not remove disk cache temporary file", "result", "cleanup_error")
	}
}
