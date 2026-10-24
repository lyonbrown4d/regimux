package object

type diskCacheKeyState struct {
	generation uint64
	users      int
	mutations  int
}

type diskCacheKeyToken struct {
	digest     string
	state      *diskCacheKeyState
	generation uint64
	blocked    bool
	released   bool
}

func (s *DiskCacheStore) acquireKeyTokenLocked(digest string) *diskCacheKeyToken {
	state := s.keyStates[digest]
	if state == nil {
		state = &diskCacheKeyState{}
		s.keyStates[digest] = state
	}
	state.users++
	return &diskCacheKeyToken{
		digest:     digest,
		state:      state,
		generation: state.generation,
		blocked:    state.mutations > 0,
	}
}

func (s *DiskCacheStore) beginKeyMutation(rawDigest string) (*diskCacheKeyToken, error) {
	digest, err := normalizeDigest(rawDigest)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errDiskCacheClosed
	}
	state := s.keyStates[digest]
	if state == nil {
		state = &diskCacheKeyState{}
		s.keyStates[digest] = state
	}
	state.users++
	state.generation++
	state.mutations++
	if fill := s.fills[digest]; fill != nil {
		fill.invalid = true
	}
	if entry := s.entries[digest]; entry != nil {
		entry.evictPending = true
		if entry.pins == 0 {
			if removeErr := s.removeEntryLocked(digest, entry); removeErr != nil {
				s.logger().Warn("could not invalidate disk cache entry before canonical mutation", "result", "error")
			}
		}
	}
	return &diskCacheKeyToken{digest: digest, state: state, generation: state.generation}, nil
}

func (s *DiskCacheStore) finishKeyMutation(token *diskCacheKeyToken) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == nil || token.released || s.keyStates[token.digest] != token.state {
		return
	}
	token.state.generation++
	token.state.mutations--
	s.releaseKeyTokenLocked(token)
}

func (s *DiskCacheStore) keyTokenValidLocked(token *diskCacheKeyToken) bool {
	return token != nil && !token.released && !token.blocked && !s.closed &&
		s.keyStates[token.digest] == token.state && token.state.generation == token.generation &&
		token.state.mutations == 0
}

func (s *DiskCacheStore) releaseKeyTokenLocked(token *diskCacheKeyToken) {
	if token == nil || token.released {
		return
	}
	token.released = true
	token.state.users--
	if token.state.users <= 0 && token.state.mutations == 0 && s.keyStates[token.digest] == token.state {
		delete(s.keyStates, token.digest)
	}
}
