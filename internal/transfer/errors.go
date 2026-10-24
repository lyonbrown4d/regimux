package transfer

import "errors"

var (
	// ErrObjectOpenTimeout identifies expiration of an individual object-open attempt.
	ErrObjectOpenTimeout = errors.New("artifact object open timeout")
	// ErrContinuationFailed identifies a failed continuation while the caller remains active.
	ErrContinuationFailed = errors.New("artifact continuation failed")
)
