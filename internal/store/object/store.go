package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

var (
	ErrNotFound       = errors.New("object not found")
	ErrDigestMismatch = errors.New("object digest mismatch")
)

// Store is the backend-independent content-addressable object storage contract.
type Store interface {
	Stat(ctx context.Context, digest string) (*Info, error)
	Exists(ctx context.Context, digest string) (bool, error)
	Get(ctx context.Context, digest string, opts GetOptions) (io.ReadCloser, *Info, error)
	Put(ctx context.Context, digest string, r io.Reader, opts PutOptions) (*Info, error)
	Delete(ctx context.Context, digest string) error
}

// ObjectWalker is implemented by stores that can enumerate CAS objects.
type ObjectWalker interface {
	WalkObjects(ctx context.Context, fn func(Info) error) error
}

// ObjectLister is implemented by stores that can return all CAS objects.
type ObjectLister interface {
	ListObjects(ctx context.Context) ([]Info, error)
}

func New(driver, path string) (Store, error) {
	return NewWithOptions(context.Background(), Options{Driver: driver, Path: path})
}

type Options struct {
	Driver string
	Path   string
	S3     S3Options
}

type S3Options struct {
	Bucket            string
	Prefix            string
	Region            string
	Endpoint          string
	AccessKeyID       string
	SecretAccessKey   string
	SessionToken      string
	Profile           string
	ForcePathStyle    bool
	PartSize          int64
	UploadConcurrency int
}

func NewWithOptions(ctx context.Context, opts Options) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(opts.Driver)) {
	case "", "local":
		return NewLocal(opts.Path)
	case "s3":
		return NewS3(ctx, opts.S3)
	default:
		return nil, fmt.Errorf("unsupported object store driver %q", opts.Driver)
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
	Range *HTTPRange
}

type PutOptions struct {
	ContentType string
	Metadata    map[string]string
}
