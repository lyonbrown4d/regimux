package object_test

import (
	"bytes"
	"context"
	"maps"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func (c *fakeS3Client) CreateMultipartUpload(
	ctx context.Context,
	input *s3.CreateMultipartUploadInput,
	_ ...func(*s3.Options),
) (*s3.CreateMultipartUploadOutput, error) {
	if err := fakeContextError(ctx); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.nextID++
	uploadID := strconv.Itoa(c.nextID)
	c.uploads[uploadID] = &fakeS3Upload{
		key:         aws.ToString(input.Key),
		contentType: aws.ToString(input.ContentType),
		metadata:    maps.Clone(input.Metadata),
		parts:       make(map[int32][]byte),
	}

	return &s3.CreateMultipartUploadOutput{UploadId: aws.String(uploadID)}, nil
}

func (c *fakeS3Client) UploadPart(
	ctx context.Context,
	input *s3.UploadPartInput,
	_ ...func(*s3.Options),
) (*s3.UploadPartOutput, error) {
	if err := fakeContextError(ctx); err != nil {
		return nil, err
	}

	body, err := readFakeBody(input.Body)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	upload := c.uploads[aws.ToString(input.UploadId)]
	if upload == nil {
		return nil, fakeS3Error("NoSuchUpload")
	}

	upload.parts[aws.ToInt32(input.PartNumber)] = bytes.Clone(body)

	return &s3.UploadPartOutput{ETag: aws.String(fakeETag(body))}, nil
}

func (c *fakeS3Client) CompleteMultipartUpload(
	ctx context.Context,
	input *s3.CompleteMultipartUploadInput,
	_ ...func(*s3.Options),
) (*s3.CompleteMultipartUploadOutput, error) {
	if err := fakeContextError(ctx); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	uploadID := aws.ToString(input.UploadId)
	upload := c.uploads[uploadID]
	if upload == nil {
		return nil, fakeS3Error("NoSuchUpload")
	}

	var body []byte
	for _, completedPart := range input.MultipartUpload.Parts {
		partNumber := aws.ToInt32(completedPart.PartNumber)
		body = append(body, upload.parts[partNumber]...)
	}

	stored := fakeS3Object{
		body:        bytes.Clone(body),
		contentType: upload.contentType,
		metadata:    maps.Clone(upload.metadata),
		etag:        fakeETag(body),
	}
	c.objects[upload.key] = stored
	delete(c.uploads, uploadID)

	return &s3.CompleteMultipartUploadOutput{
		ETag: aws.String(stored.etag),
		Key:  aws.String(upload.key),
	}, nil
}

func (c *fakeS3Client) AbortMultipartUpload(
	ctx context.Context,
	input *s3.AbortMultipartUploadInput,
	_ ...func(*s3.Options),
) (*s3.AbortMultipartUploadOutput, error) {
	if err := fakeContextError(ctx); err != nil {
		return nil, err
	}

	c.mu.Lock()
	delete(c.uploads, aws.ToString(input.UploadId))
	c.mu.Unlock()

	return &s3.AbortMultipartUploadOutput{}, nil
}
