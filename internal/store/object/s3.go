package object

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Client is the subset of the native AWS S3 client used by S3Store.
type S3Client interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	CreateMultipartUpload(
		context.Context,
		*s3.CreateMultipartUploadInput,
		...func(*s3.Options),
	) (*s3.CreateMultipartUploadOutput, error)
	UploadPart(context.Context, *s3.UploadPartInput, ...func(*s3.Options)) (*s3.UploadPartOutput, error)
	CompleteMultipartUpload(
		context.Context,
		*s3.CompleteMultipartUploadInput,
		...func(*s3.Options),
	) (*s3.CompleteMultipartUploadOutput, error)
	AbortMultipartUpload(
		context.Context,
		*s3.AbortMultipartUploadInput,
		...func(*s3.Options),
	) (*s3.AbortMultipartUploadOutput, error)
}

// S3Store stores CAS objects directly through the AWS S3 API.
type S3Store struct {
	client            S3Client
	bucket            string
	prefix            string
	partSize          int64
	uploadConcurrency int
}

func NewS3(ctx context.Context, rawOptions S3Options) (*S3Store, error) {
	options, err := normalizeS3Options(rawOptions)
	if err != nil {
		return nil, err
	}

	awsConfig, err := loadAWSConfig(ctx, options)
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(awsConfig, func(clientOptions *s3.Options) {
		clientOptions.UsePathStyle = options.ForcePathStyle
		if options.Endpoint != "" {
			clientOptions.BaseEndpoint = aws.String(options.Endpoint)
		}
	})

	return NewS3WithClient(client, options)
}

// NewS3WithClient constructs an S3 store with an injected native S3 client.
func NewS3WithClient(client S3Client, rawOptions S3Options) (*S3Store, error) {
	if client == nil {
		return nil, errors.New("S3 client is required")
	}

	options, err := normalizeS3Options(rawOptions)
	if err != nil {
		return nil, err
	}

	return &S3Store{
		client:            client,
		bucket:            options.Bucket,
		prefix:            options.Prefix,
		partSize:          options.PartSize,
		uploadConcurrency: options.UploadConcurrency,
	}, nil
}

func (s *S3Store) Stat(ctx context.Context, rawDigest string) (*Info, error) {
	normalized, key, err := casObjectKey(s.prefix, rawDigest)
	if err != nil {
		return nil, err
	}

	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, wrapS3OperationError("stat", normalized, err)
	}

	return &Info{
		Digest:      normalized,
		Size:        aws.ToInt64(output.ContentLength),
		ContentType: aws.ToString(output.ContentType),
		ETag:        normalizeETag(aws.ToString(output.ETag)),
		Path:        key,
	}, nil
}

func (s *S3Store) Exists(ctx context.Context, rawDigest string) (bool, error) {
	_, err := s.Stat(ctx, rawDigest)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}

	return false, err
}

func (s *S3Store) Get(
	ctx context.Context,
	rawDigest string,
	opts GetOptions,
) (io.ReadCloser, *Info, error) {
	info, err := s.Stat(ctx, rawDigest)
	if err != nil {
		return nil, nil, err
	}

	input, err := s.getObjectInput(info, opts.Range)
	if err != nil {
		return nil, nil, err
	}

	output, err := s.client.GetObject(ctx, input)
	if err != nil {
		return nil, nil, wrapS3OperationError("get", info.Digest, err)
	}
	if output.Body == nil {
		return nil, nil, fmt.Errorf("get S3 object %q: response body is nil", info.Digest)
	}

	resultInfo := s.getResultInfo(info, output)

	return &contextReadCloser{
		ctx:    ctx,
		reader: output.Body,
		closer: output.Body,
	}, resultInfo, nil
}

func (s *S3Store) getObjectInput(
	info *Info,
	requested *HTTPRange,
) (*s3.GetObjectInput, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(info.Path),
	}
	if requested == nil {
		return input, nil
	}

	resolved, err := requested.Resolve(info.Size)
	if err != nil {
		return nil, err
	}
	input.Range = aws.String(resolved.String())

	return input, nil
}

func (s *S3Store) getResultInfo(info *Info, output *s3.GetObjectOutput) *Info {
	result := *info
	result.Size = aws.ToInt64(output.ContentLength)
	if output.ContentType != nil {
		result.ContentType = aws.ToString(output.ContentType)
	}
	if output.ETag != nil {
		result.ETag = normalizeETag(aws.ToString(output.ETag))
	}

	return &result
}

func (s *S3Store) Put(
	ctx context.Context,
	rawDigest string,
	reader io.Reader,
	opts PutOptions,
) (*Info, error) {
	normalized, key, err := casObjectKey(s.prefix, rawDigest)
	if err != nil {
		return nil, err
	}

	existing, err := s.Stat(ctx, normalized)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	upload, err := s.upload(ctx, normalized, key, reader, opts)
	if err != nil {
		return nil, wrapS3OperationError("put", normalized, err)
	}

	return &Info{
		Digest:      normalized,
		Size:        upload.size,
		ContentType: opts.ContentType,
		ETag:        normalizeETag(upload.etag),
		Path:        key,
	}, nil
}

func (s *S3Store) Delete(ctx context.Context, rawDigest string) error {
	normalized, key, err := casObjectKey(s.prefix, rawDigest)
	if err != nil {
		return err
	}

	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return wrapS3OperationError("delete", normalized, err)
	}

	return nil
}

func (s *S3Store) WalkObjects(ctx context.Context, fn func(Info) error) error {
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(casListPrefix(s.prefix)),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list S3 CAS objects: %w", err)
		}
		if err := s.walkPage(page.Contents, fn); err != nil {
			return err
		}
	}

	return nil
}

func (s *S3Store) walkPage(objects []types.Object, fn func(Info) error) error {
	for _, object := range objects {
		key := aws.ToString(object.Key)
		digest, valid := casDigestFromObjectKey(s.prefix, key)
		if !valid {
			continue
		}

		err := fn(Info{
			Digest: digest,
			Size:   aws.ToInt64(object.Size),
			ETag:   normalizeETag(aws.ToString(object.ETag)),
			Path:   key,
		})
		if err != nil {
			return fmt.Errorf("visit S3 CAS object %q: %w", digest, err)
		}
	}

	return nil
}

func (s *S3Store) ListObjects(ctx context.Context) ([]Info, error) {
	objects := make([]Info, 0)
	err := s.WalkObjects(ctx, func(info Info) error {
		objects = append(objects, info)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return objects, nil
}
