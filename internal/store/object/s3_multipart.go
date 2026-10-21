package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"sort"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"golang.org/x/sync/errgroup"
)

const s3AbortTimeout = 30 * time.Second

type multipartUpload struct {
	store     *S3Store
	ctx       context.Context
	group     *errgroup.Group
	groupCtx  context.Context
	key       string
	uploadID  string
	verifier  *digestVerifier
	parts     []types.CompletedPart
	partsMu   sync.Mutex
	completed bool
}

func (s *S3Store) uploadMultipart(
	ctx context.Context,
	key string,
	source io.ReaderAt,
	verifier *digestVerifier,
	opts PutOptions,
) (result *s3UploadResult, resultErr error) {
	upload, err := s.startMultipart(ctx, key, verifier, opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = upload.abort(ctx, resultErr)
	}()

	if uploadErr := upload.uploadParts(source); uploadErr != nil {
		return nil, uploadErr
	}
	if verifyErr := verifier.verify(); verifyErr != nil {
		return nil, verifyErr
	}

	result, err = upload.complete()
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *S3Store) startMultipart(
	ctx context.Context,
	key string,
	verifier *digestVerifier,
	opts PutOptions,
) (*multipartUpload, error) {
	input := &s3.CreateMultipartUploadInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(key),
		Metadata: maps.Clone(opts.Metadata),
	}
	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}

	output, err := s.client.CreateMultipartUpload(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("create S3 multipart upload: %w", err)
	}

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(s.uploadConcurrency)

	return &multipartUpload{
		store:    s,
		ctx:      ctx,
		group:    group,
		groupCtx: groupCtx,
		key:      key,
		uploadID: aws.ToString(output.UploadId),
		verifier: verifier,
		parts:    make([]types.CompletedPart, 0, 8),
	}, nil
}

func (u *multipartUpload) uploadParts(
	source io.ReaderAt,
) error {
	readErr := u.submitParts(source)
	waitErr := u.wait()

	return errors.Join(readErr, waitErr)
}

func (u *multipartUpload) submitParts(source io.ReaderAt) error {
	partNumber := int32(1)
	for offset := int64(0); offset < u.verifier.sizeValue(); offset += u.store.partSize {
		if err := u.groupCtx.Err(); err != nil {
			return fmt.Errorf("upload S3 parts: %w", err)
		}
		if partNumber > maxS3UploadParts {
			return fmt.Errorf("S3 multipart upload exceeds %d parts", maxS3UploadParts)
		}
		size := min(u.store.partSize, u.verifier.sizeValue()-offset)
		u.submit(partNumber, io.NewSectionReader(source, offset, size), size)
		partNumber++
	}
	return nil
}

func (u *multipartUpload) submit(number int32, body io.Reader, size int64) {
	u.group.Go(func() error {
		output, err := u.store.client.UploadPart(u.groupCtx, &s3.UploadPartInput{
			Body:          body,
			ContentLength: aws.Int64(size),
			Bucket:        aws.String(u.store.bucket),
			Key:           aws.String(u.key),
			PartNumber:    aws.Int32(number),
			UploadId:      aws.String(u.uploadID),
		})
		if err != nil {
			return fmt.Errorf("upload S3 part %d: %w", number, err)
		}

		u.partsMu.Lock()
		u.parts = append(u.parts, types.CompletedPart{
			ETag:       output.ETag,
			PartNumber: aws.Int32(number),
		})
		u.partsMu.Unlock()

		return nil
	})
}

func (u *multipartUpload) wait() error {
	if err := u.group.Wait(); err != nil {
		return fmt.Errorf("wait for S3 multipart upload: %w", err)
	}

	return nil
}

func (u *multipartUpload) complete() (*s3UploadResult, error) {
	sort.Slice(u.parts, func(left, right int) bool {
		return aws.ToInt32(u.parts[left].PartNumber) < aws.ToInt32(u.parts[right].PartNumber)
	})

	output, err := u.store.client.CompleteMultipartUpload(
		u.ctx,
		&s3.CompleteMultipartUploadInput{
			Bucket:   aws.String(u.store.bucket),
			Key:      aws.String(u.key),
			UploadId: aws.String(u.uploadID),
			MultipartUpload: &types.CompletedMultipartUpload{
				Parts: u.parts,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("complete S3 multipart upload: %w", err)
	}

	u.completed = true

	return &s3UploadResult{
		etag: aws.ToString(output.ETag),
		size: u.verifier.sizeValue(),
	}, nil
}

func (u *multipartUpload) abort(ctx context.Context, resultErr error) error {
	if u.completed {
		return resultErr
	}

	abortCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		s3AbortTimeout,
	)
	defer cancel()

	_, err := u.store.client.AbortMultipartUpload(
		abortCtx,
		&s3.AbortMultipartUploadInput{
			Bucket:   aws.String(u.store.bucket),
			Key:      aws.String(u.key),
			UploadId: aws.String(u.uploadID),
		},
	)
	if err != nil {
		return errors.Join(
			resultErr,
			fmt.Errorf("abort S3 multipart upload: %w", err),
		)
	}

	return resultErr
}
