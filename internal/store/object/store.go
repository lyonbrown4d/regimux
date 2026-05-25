package object

import (
	"context"
	"io"

	"github.com/lyonbrown4d/regimux/internal/reference"
)

type Store interface {
	Stat(ctx context.Context, key string) (*Info, error)
	Get(ctx context.Context, key string, opts GetOptions) (io.ReadCloser, *Info, error)
	Put(ctx context.Context, key string, r io.Reader, opts PutOptions) (*Info, error)
	Delete(ctx context.Context, key string) error
}

type Info struct {
	Key         string
	Size        int64
	ContentType string
	ETag        string
}

type GetOptions struct {
	Range *reference.HTTPRange
}

type PutOptions struct {
	Size        int64
	ContentType string
	Metadata    map[string]string
}

