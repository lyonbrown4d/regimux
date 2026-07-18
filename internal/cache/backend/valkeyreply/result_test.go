package valkeyreply_test

import (
	"errors"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/cache/backend/valkeyreply"
)

func TestParseLockPrefersIntegerReply(t *testing.T) {
	t.Parallel()

	reply := &stubResult{
		integer:   1,
		stringErr: errors.New("int64 is not a string"),
	}
	held, err := valkeyreply.ParseLock(reply)
	if err != nil {
		t.Fatalf("ParseLock() error = %v", err)
	}
	if !held {
		t.Fatal("ParseLock() = false, want true")
	}
	if reply.stringCalls != 0 {
		t.Fatalf("ToString() calls = %d, want 0", reply.stringCalls)
	}
}

func TestParseLockHandlesZeroIntegerReply(t *testing.T) {
	t.Parallel()

	reply := &stubResult{integer: 0}
	held, err := valkeyreply.ParseLock(reply)
	if err != nil {
		t.Fatalf("ParseLock() error = %v", err)
	}
	if held {
		t.Fatal("ParseLock() = true, want false")
	}
	if reply.stringCalls != 0 {
		t.Fatalf("ToString() calls = %d, want 0", reply.stringCalls)
	}
}

func TestParseLockSupportsStringReply(t *testing.T) {
	t.Parallel()

	reply := &stubResult{
		integerErr: errors.New("reply is not an integer"),
		text:       "1",
	}
	held, err := valkeyreply.ParseLock(reply)
	if err != nil {
		t.Fatalf("ParseLock() error = %v", err)
	}
	if !held {
		t.Fatal("ParseLock() = false, want true")
	}
	if reply.stringCalls != 1 {
		t.Fatalf("ToString() calls = %d, want 1", reply.stringCalls)
	}
}

type stubResult struct {
	commandErr  error
	integer     int64
	integerErr  error
	text        string
	stringErr   error
	stringCalls int
}

func (r *stubResult) Error() error {
	return r.commandErr
}

func (r *stubResult) AsInt64() (int64, error) {
	return r.integer, r.integerErr
}

func (r *stubResult) ToString() (string, error) {
	r.stringCalls++
	return r.text, r.stringErr
}
