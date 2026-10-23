package upstream

import (
	"errors"
	"sync"
)

var errBlobAttemptUnselected = errors.New("blob attempt was not selected")

type blobResponseSelection struct {
	mu       sync.Mutex
	selected *BlobResponse
	aborted  bool
}

func (s *blobResponseSelection) selectResponse(response *BlobResponse) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aborted || s.selected != nil {
		return false
	}
	s.selected = response
	return true
}

func (s *blobResponseSelection) response() *BlobResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aborted {
		return nil
	}
	return s.selected
}

func (s *blobResponseSelection) hasResponse() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selected != nil && !s.aborted
}

func (s *blobResponseSelection) abort() error {
	s.mu.Lock()
	s.aborted = true
	response := s.selected
	s.selected = nil
	s.mu.Unlock()
	if response != nil && response.Body != nil {
		return closeBody(response.Body)
	}
	return nil
}

func closeUnselectedBlob(response *BlobResponse) error {
	if response != nil && response.Body != nil {
		return closeBodyWithError(response.Body, errBlobAttemptUnselected)
	}
	return errBlobAttemptUnselected
}
