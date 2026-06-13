package object

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	aferos3 "github.com/fclairamb/afero-s3"
)

type S3Store struct {
	*aferoStore
}

var _ Store = (*S3Store)(nil)
var _ ObjectWalker = (*S3Store)(nil)
var _ ObjectLister = (*S3Store)(nil)

func NewS3(ctx context.Context, opts S3Options) (*S3Store, error) {
	ctx = normalizeContext(ctx)
	if err := checkContext(ctx, "create s3 object store"); err != nil {
		return nil, err
	}
	opts = normalizeS3Options(opts)
	if opts.Bucket == "" {
		return nil, errorf("s3 object store bucket is required")
	}
	if opts.Region == "" {
		return nil, errorf("s3 object store region is required")
	}
	cfg, err := loadAWSConfig(ctx, opts)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		if opts.Endpoint != "" {
			options.BaseEndpoint = aws.String(opts.Endpoint)
		}
		options.UsePathStyle = opts.ForcePathStyle
	})
	store, err := newAferoStore(newSlashPathFS(aferos3.NewFsFromClient(opts.Bucket, client)), s3BasePath(opts.Prefix), true, false)
	if err != nil {
		return nil, err
	}
	return &S3Store{aferoStore: store}, nil
}

func (s *S3Store) WalkObjects(ctx context.Context, fn ObjectWalkFunc) error {
	if s == nil || s.aferoStore == nil {
		return errorf("object store is not configured")
	}
	return s.walkObjects(ctx, fn)
}

func (s *S3Store) ListObjects(ctx context.Context) ([]Info, error) {
	if s == nil || s.aferoStore == nil {
		return nil, errorf("object store is not configured")
	}
	return s.listObjects(ctx)
}

func normalizeS3Options(opts S3Options) S3Options {
	opts.Bucket = strings.TrimSpace(opts.Bucket)
	opts.Prefix = strings.Trim(strings.TrimSpace(opts.Prefix), "/")
	opts.Region = strings.TrimSpace(opts.Region)
	opts.Endpoint = strings.TrimSpace(opts.Endpoint)
	opts.AccessKeyID = strings.TrimSpace(opts.AccessKeyID)
	opts.SecretAccessKey = strings.TrimSpace(opts.SecretAccessKey)
	opts.SessionToken = strings.TrimSpace(opts.SessionToken)
	opts.Profile = strings.TrimSpace(opts.Profile)
	return opts
}

func s3BasePath(prefix string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return "."
	}
	return prefix
}

func loadAWSConfig(ctx context.Context, opts S3Options) (aws.Config, error) {
	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(opts.Region),
	}
	if opts.Profile != "" {
		loadOpts = append(loadOpts, awsconfig.WithSharedConfigProfile(opts.Profile))
	}
	if opts.AccessKeyID != "" || opts.SecretAccessKey != "" || opts.SessionToken != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(opts.AccessKeyID, opts.SecretAccessKey, opts.SessionToken),
		))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return aws.Config{}, wrapError(err, "load aws config for s3 object store")
	}
	return cfg, nil
}
