package artifactcache

// ForgetMetadata prevents new readers from joining a snapshot started before a metadata write.
// Call it after committing metadata through a storage path outside Store.Put.
func ForgetMetadata(s *Store, key Key) {
	if s != nil {
		s.reads.Forget(key)
	}
}
