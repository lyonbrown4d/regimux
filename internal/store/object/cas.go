// Package object provides content-addressable local and S3 object stores.
package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

	godigest "github.com/opencontainers/go-digest"
)

const casRootDirectory = "blobs"

func casRelativePath(rawDigest string) (string, string, error) {
	normalized, err := normalizeDigest(rawDigest)
	if err != nil {
		return "", "", err
	}

	parts := strings.SplitN(normalized, ":", 2)
	encoded := parts[1]

	return normalized, path.Join(casRootDirectory, parts[0], encoded[:2], encoded), nil
}

func casObjectKey(prefix, rawDigest string) (string, string, error) {
	normalized, relative, err := casRelativePath(rawDigest)
	if err != nil {
		return "", "", err
	}

	prefix = normalizeObjectPrefix(prefix)
	if prefix == "" {
		return normalized, relative, nil
	}

	return normalized, path.Join(prefix, relative), nil
}

func casListPrefix(prefix string) string {
	prefix = normalizeObjectPrefix(prefix)
	if prefix == "" {
		return casRootDirectory + "/"
	}

	return path.Join(prefix, casRootDirectory) + "/"
}

func casDigestFromRelativePath(relative string) (string, bool) {
	if relative == "" || strings.HasPrefix(relative, "/") || path.Clean(relative) != relative {
		return "", false
	}

	parts := strings.Split(relative, "/")
	if len(parts) != 4 || parts[0] != casRootDirectory {
		return "", false
	}

	encoded := parts[3]
	if len(encoded) < 2 || parts[2] != encoded[:2] {
		return "", false
	}

	candidate := parts[1] + ":" + encoded
	normalized, err := normalizeDigest(candidate)
	if err != nil || normalized != candidate {
		return "", false
	}

	return normalized, true
}

func casDigestFromObjectKey(prefix, key string) (string, bool) {
	prefix = normalizeObjectPrefix(prefix)
	if prefix != "" {
		objectPrefix := prefix + "/"
		if !strings.HasPrefix(key, objectPrefix) {
			return "", false
		}
		key = strings.TrimPrefix(key, objectPrefix)
	}

	return casDigestFromRelativePath(key)
}

func normalizeObjectPrefix(prefix string) string {
	cleaned := path.Clean("/" + strings.TrimSpace(prefix))
	return strings.Trim(cleaned, "/")
}

type digestVerifier struct {
	mu       sync.Mutex
	digester godigest.Digester
	expected string
	size     int64
}

func newDigestVerifier(expected string) *digestVerifier {
	parsed := godigest.Digest(expected)

	return &digestVerifier{
		digester: parsed.Algorithm().Digester(),
		expected: expected,
	}
}

func (v *digestVerifier) Write(data []byte) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	written, err := v.digester.Hash().Write(data)
	v.size += int64(written)
	if err != nil {
		return written, fmt.Errorf("hash object data: %w", err)
	}

	return written, nil
}

func (v *digestVerifier) verify() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	actual := v.digester.Digest().String()
	if actual != v.expected {
		return NewDigestMismatch(v.expected, actual)
	}

	return nil
}

func (v *digestVerifier) sizeValue() int64 {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.size
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	return readWithContext(r.ctx, r.reader, buffer)
}

type contextReadCloser struct {
	ctx    context.Context
	reader io.Reader
	closer io.Closer
}

func (r *contextReadCloser) Read(buffer []byte) (int, error) {
	return readWithContext(r.ctx, r.reader, buffer)
}

func (r *contextReadCloser) Close() error {
	if err := r.closer.Close(); err != nil {
		return fmt.Errorf("close object reader: %w", err)
	}

	return nil
}

func readWithContext(ctx context.Context, reader io.Reader, buffer []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("read object data: %w", err)
	}

	read, err := reader.Read(buffer)
	if err == nil {
		return read, nil
	}
	if errors.Is(err, io.EOF) {
		return read, io.EOF
	}

	return read, fmt.Errorf("read object data: %w", err)
}
