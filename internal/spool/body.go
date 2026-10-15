// Package spool contains temporary-body helpers for upstream responses.
package spool

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

type TempBody struct {
	*os.File
	name string
}

// NewTempBody creates a writable spool whose file is removed when it is closed.
func NewTempBody(pattern string) (*TempBody, error) {
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return nil, wrapError(err, "create temp file")
	}
	return &TempBody{File: tmp, name: tmp.Name()}, nil
}

func MaterializeReadCloser(body io.ReadCloser, pattern string) (io.ReadCloser, error) {
	if body == nil {
		return http.NoBody, nil
	}
	tmp, err := NewTempBody(pattern)
	if err != nil {
		return nil, wrapError(err, "create temp file")
	}
	if _, err := io.Copy(tmp, body); err != nil {
		return nil, errors.Join(err, tmp.Close())
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, errors.Join(err, tmp.Close())
	}
	return tmp, nil
}

func (t *TempBody) Close() error {
	if t == nil || t.File == nil {
		return nil
	}
	return wrapError(errors.Join(t.File.Close(), os.Remove(t.name)), "close and remove temp body")
}

func wrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}
