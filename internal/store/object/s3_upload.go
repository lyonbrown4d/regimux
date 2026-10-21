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
	"github.com/lyonbrown4d/regimux/internal/spool"
)

const s3UploadProbeSize = 32 << 10

type s3UploadResult struct {
	etag string
	size int64
}

func (s *S3Store) upload(
	ctx context.Context,
	digest, key string,
	reader io.Reader,
	opts PutOptions,
) (result *s3UploadResult, resultErr error) {
	if owned, ok := reader.(immutableFileReader); ok {
		return s.uploadSeekable(ctx, digest, key, owned, opts)
	}
	verifier := newDigestVerifier(digest)
	source := io.TeeReader(&contextReader{ctx: ctx, reader: reader}, verifier)

	// A small object never allocates or retains a multipart-sized backing array.
	firstChunk, exhausted, err := readS3Probe(source)
	if err != nil {
		return nil, err
	}
	if exhausted {
		return s.uploadSingle(ctx, key, bytes.NewReader(firstChunk), verifier, opts)
	}
	// Seekable file sections let the SDK retry parts without retaining part-sized
	// byte slices. Keep the file alive until all part workers have finished.
	body, err := spool.NewTempBody("regimux-s3-upload-*")
	if err != nil {
		return nil, fmt.Errorf("create S3 upload spool: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, body.Close()) }()
	if err := copyS3Upload(body, io.MultiReader(bytes.NewReader(firstChunk), source), s.maxUploadSize()); err != nil {
		return nil, err
	}
	if err := verifier.verify(); err != nil {
		return nil, err
	}
	return s.uploadVerified(ctx, key, body, verifier, opts)
}

func (s *S3Store) maxUploadSize() int64 { return s.partSize * maxS3UploadParts }

func copyS3Upload(destination io.Writer, source io.Reader, maxBytes int64) error {
	size, err := io.Copy(destination, io.LimitReader(source, maxBytes+1))
	if err != nil {
		return fmt.Errorf("read S3 upload: %w", err)
	}
	if size > maxBytes {
		return fmt.Errorf("S3 multipart upload exceeds %d parts", maxS3UploadParts)
	}
	return nil
}

func (s *S3Store) uploadSingle(
	ctx context.Context,
	key string,
	body io.Reader,
	verifier *digestVerifier,
	opts PutOptions,
) (*s3UploadResult, error) {
	if err := verifier.verify(); err != nil {
		return nil, err
	}

	input := s.putObjectInput(key, body, opts)
	input.ContentLength = aws.Int64(verifier.sizeValue())
	output, err := s.client.PutObject(
		ctx,
		input,
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

func readS3Probe(reader io.Reader) ([]byte, bool, error) {
	buffer := make([]byte, s3UploadProbeSize+1)
	read, err := io.ReadFull(reader, buffer)
	if err == nil {
		return buffer[:read], false, nil
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return buffer[:read], true, nil
	}

	return nil, false, fmt.Errorf("read S3 upload data: %w", err)
}
