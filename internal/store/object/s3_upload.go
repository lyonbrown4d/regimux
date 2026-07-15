package object

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3UploadResult struct {
	etag string
	size int64
}

func (s *S3Store) upload(
	ctx context.Context,
	digest, key string,
	reader io.Reader,
	opts PutOptions,
) (*s3UploadResult, error) {
	verifier := newDigestVerifier(digest)
	source := io.TeeReader(&contextReader{ctx: ctx, reader: reader}, verifier)

	firstChunk, _, err := readS3Chunk(source, s.partSize+1, nil)
	if err != nil {
		return nil, err
	}
	if int64(len(firstChunk)) <= s.partSize {
		return s.uploadSingle(ctx, key, firstChunk, verifier, opts)
	}

	partSize := int(s.partSize)

	return s.uploadMultipart(
		ctx,
		key,
		source,
		verifier,
		firstChunk[:partSize],
		firstChunk[partSize:],
		opts,
	)
}

func (s *S3Store) uploadSingle(
	ctx context.Context,
	key string,
	data []byte,
	verifier *digestVerifier,
	opts PutOptions,
) (*s3UploadResult, error) {
	if err := verifier.verify(); err != nil {
		return nil, err
	}

	output, err := s.client.PutObject(
		ctx,
		s.putObjectInput(key, bytes.NewReader(data), opts),
	)
	if err != nil {
		return nil, fmt.Errorf("upload S3 object: %w", err)
	}

	return &s3UploadResult{
		etag: aws.ToString(output.ETag),
		size: verifier.sizeValue(),
	}, nil
}

func (s *S3Store) putObjectInput(
	key string,
	body io.Reader,
	opts PutOptions,
) *s3.PutObjectInput {
	input := &s3.PutObjectInput{
		Body:     body,
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(key),
		Metadata: maps.Clone(opts.Metadata),
	}
	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}

	return input
}

func readS3Chunk(reader io.Reader, size int64, prefix []byte) ([]byte, bool, error) {
	if size <= 0 || size > int64(^uint(0)>>1) {
		return nil, false, fmt.Errorf("unsupported S3 read buffer size %d", size)
	}
	if int64(len(prefix)) > size {
		return nil, false, fmt.Errorf("S3 read prefix exceeds buffer size %d", size)
	}

	buffer := make([]byte, int(size))
	written := copy(buffer, prefix)
	read, err := io.ReadFull(reader, buffer[written:])
	written += read

	if err == nil {
		return buffer[:written], false, nil
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return buffer[:written], true, nil
	}

	return nil, false, fmt.Errorf("read S3 upload data: %w", err)
}
