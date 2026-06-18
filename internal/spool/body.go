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

func MaterializeReadCloser(body io.ReadCloser, pattern string) (io.ReadCloser, error) {
	if body == nil {
		return http.NoBody, nil
	}
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return nil, wrapError(err, "create temp file")
	}
	name := tmp.Name()
	if _, err := io.Copy(tmp, body); err != nil {
		return nil, closeAndRemoveTemp(tmp, name, err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, closeAndRemoveTemp(tmp, name, err)
	}
	return &TempBody{File: tmp, name: name}, nil
}

func (t *TempBody) Close() error {
	if t == nil || t.File == nil {
		return nil
	}
	return wrapError(errors.Join(t.File.Close(), os.Remove(t.name)), "close and remove temp body")
}

func closeAndRemoveTemp(file *os.File, name string, err error) error {
	return wrapError(errors.Join(err, file.Close(), os.Remove(name)), "close and remove temp file")
}

func wrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}
