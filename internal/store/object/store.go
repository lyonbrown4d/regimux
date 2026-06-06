package object

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
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
	return NewWithOptions(context.Background(), Options{Driver: driver, Path: path})
}

type Options struct {
	Driver string
	Path   string
	S3     S3Options
	SFTP   SFTPOptions
}

type S3Options struct {
	Bucket          string
	Prefix          string
	Region          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Profile         string
	ForcePathStyle  bool
}

type SFTPOptions struct {
	Addr                 string
	Username             string
	Password             string
	PrivateKey           string
	PrivateKeyPassphrase string
	KnownHostsPath       string
	HostKey              string
	Timeout              time.Duration
}

func NewWithOptions(ctx context.Context, opts Options) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(opts.Driver)) {
	case "", "local":
		return NewLocal(opts.Path)
	case "memory":
		return NewMemory(opts.Path)
	case "s3":
		return NewS3(ctx, opts.S3)
	case "sftp":
		return NewSFTP(ctx, opts.Path, opts.SFTP)
	default:
		return nil, errorf("unsupported object store driver: %s", opts.Driver)
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
