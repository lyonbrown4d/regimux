package object

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func (s *S3Store) Get(ctx context.Context, rawDigest string, opts GetOptions) (io.ReadCloser, *Info, error) {
	info, err := s.getReadInfo(ctx, rawDigest, opts.Range)
	if err != nil {
		return nil, nil, err
	}
	input, err := s.getObjectInput(info, opts.Range)
	if err != nil {
		return nil, nil, err
	}
	output, err := s.client.GetObject(ctx, input)
	if err != nil {
		if output != nil && output.Body != nil {
			err = errors.Join(err, output.Body.Close())
		}
		return nil, nil, wrapS3OperationError("get", info.Digest, err)
	}
	if output == nil || output.Body == nil {
		return nil, nil, fmt.Errorf("get S3 object %q: response body is nil", info.Digest)
	}
	return &contextReadCloser{
		ctx: ctx, reader: output.Body, closer: output.Body,
	}, s.getResultInfo(info, output), nil
}

func (s *S3Store) getReadInfo(ctx context.Context, rawDigest string, requested *HTTPRange) (*Info, error) {
	if requested != nil {
		return s.Stat(ctx, rawDigest)
	}
	// A full GetObject response includes its own length, type and ETag. Only
	// range resolution needs the complete size from a preceding HeadObject.
	normalized, key, err := casObjectKey(s.prefix, rawDigest)
	if err != nil {
		return nil, err
	}
	return &Info{Digest: normalized, Path: key}, nil
}

func (s *S3Store) getObjectInput(info *Info, requested *HTTPRange) (*s3.GetObjectInput, error) {
	input := &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(info.Path)}
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
