package object

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/reference"
)

var (
	ErrNotFound       = errors.New("object not found")
	ErrDigestMismatch = errors.New("object digest mismatch")
)

type Store interface {
	Stat(ctx context.Context, digest string) (*Info, error)
	Exists(ctx context.Context, digest string) (bool, error)
	Get(ctx context.Context, digest string, opts GetOptions) (io.ReadCloser, *Info, error)
	Put(ctx context.Context, digest string, r io.Reader, opts PutOptions) (*Info, error)
	Delete(ctx context.Context, digest string) error
}

func New(driver, path string) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", "local":
		return NewLocal(path)
	case "memory":
		return NewMemory(path)
	default:
		return nil, errorf("unsupported object store driver: %s", driver)
	}
}

type Info struct {
	Digest      string
	Size        int64
	ContentType string
	ETag        string
	Path        string
}

type GetOptions struct {
	Range *reference.HTTPRange
}

type PutOptions struct {
	ContentType string
	Metadata    map[string]string
}
