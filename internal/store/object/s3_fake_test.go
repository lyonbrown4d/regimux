package object_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type fakeS3Object struct {
	body        []byte
	contentType string
	metadata    map[string]string
	etag        string
}

type fakeS3Upload struct {
	key         string
	contentType string
	metadata    map[string]string
	parts       map[int32][]byte
}

type fakeS3Client struct {
	mu      sync.Mutex
	objects map[string]fakeS3Object
	uploads map[string]*fakeS3Upload
	nextID  int
}

func newFakeS3Client() *fakeS3Client {
	return &fakeS3Client{
		objects: make(map[string]fakeS3Object),
		uploads: make(map[string]*fakeS3Upload),
	}
}

func (c *fakeS3Client) PutObject(
	ctx context.Context,
	input *s3.PutObjectInput,
	_ ...func(*s3.Options),
) (*s3.PutObjectOutput, error) {
	if err := fakeContextError(ctx); err != nil {
		return nil, err
	}

	body, err := readFakeBody(input.Body)
	if err != nil {
		return nil, err
	}
	if err := fakeContextError(ctx); err != nil {
		return nil, err
	}

	stored := fakeS3Object{
		body:        bytes.Clone(body),
		contentType: aws.ToString(input.ContentType),
		metadata:    maps.Clone(input.Metadata),
		etag:        fakeETag(body),
	}

	c.mu.Lock()
	c.objects[aws.ToString(input.Key)] = stored
	c.mu.Unlock()

	return &s3.PutObjectOutput{ETag: aws.String(stored.etag)}, nil
}

func (c *fakeS3Client) HeadObject(
	ctx context.Context,
	input *s3.HeadObjectInput,
	_ ...func(*s3.Options),
) (*s3.HeadObjectOutput, error) {
	if err := fakeContextError(ctx); err != nil {
		return nil, err
	}

	stored, found := c.object(aws.ToString(input.Key))
	if !found {
		return nil, fakeS3Error("NotFound")
	}

	return &s3.HeadObjectOutput{
		ContentLength: aws.Int64(int64(len(stored.body))),
		ContentType:   optionalString(stored.contentType),
		ETag:          aws.String(stored.etag),
		Metadata:      maps.Clone(stored.metadata),
	}, nil
}

func (c *fakeS3Client) GetObject(
	ctx context.Context,
	input *s3.GetObjectInput,
	_ ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	if err := fakeContextError(ctx); err != nil {
		return nil, err
	}

	stored, found := c.object(aws.ToString(input.Key))
	if !found {
		return nil, fakeS3Error("NoSuchKey")
	}

	body, err := applyFakeRange(stored.body, input.Range)
	if err != nil {
		return nil, err
	}

	return &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: aws.Int64(int64(len(body))),
		ContentType:   optionalString(stored.contentType),
		ETag:          aws.String(stored.etag),
		Metadata:      maps.Clone(stored.metadata),
	}, nil
}

func (c *fakeS3Client) DeleteObject(
	ctx context.Context,
	input *s3.DeleteObjectInput,
	_ ...func(*s3.Options),
) (*s3.DeleteObjectOutput, error) {
	if err := fakeContextError(ctx); err != nil {
		return nil, err
	}

	c.mu.Lock()
	delete(c.objects, aws.ToString(input.Key))
	c.mu.Unlock()

	return &s3.DeleteObjectOutput{}, nil
}

func (c *fakeS3Client) ListObjectsV2(
	ctx context.Context,
	input *s3.ListObjectsV2Input,
	_ ...func(*s3.Options),
) (*s3.ListObjectsV2Output, error) {
	if err := fakeContextError(ctx); err != nil {
		return nil, err
	}

	prefix := aws.ToString(input.Prefix)
	c.mu.Lock()
	defer c.mu.Unlock()

	keys := make([]string, 0, len(c.objects))
	for key := range c.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	objects := make([]types.Object, 0, len(keys))
	for _, key := range keys {
		stored := c.objects[key]
		objects = append(objects, types.Object{
			Key:  aws.String(key),
			Size: aws.Int64(int64(len(stored.body))),
			ETag: aws.String(stored.etag),
		})
	}

	return &s3.ListObjectsV2Output{
		Contents:    objects,
		IsTruncated: aws.Bool(false),
	}, nil
}

func (c *fakeS3Client) object(key string) (fakeS3Object, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stored, found := c.objects[key]
	if !found {
		return fakeS3Object{}, false
	}

	stored.body = bytes.Clone(stored.body)
	stored.metadata = maps.Clone(stored.metadata)

	return stored, true
}

func (c *fakeS3Client) uploadCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.uploads)
}

func applyFakeRange(body []byte, requested *string) ([]byte, error) {
	if requested == nil {
		return body, nil
	}

	start, end, err := fakeConcreteRange(aws.ToString(requested), int64(len(body)))
	if err != nil {
		return nil, err
	}

	return body[start : end+1], nil
}

func fakeConcreteRange(value string, size int64) (int64, int64, error) {
	value = strings.TrimPrefix(value, "bytes=")
	startValue, endValue, found := strings.Cut(value, "-")
	if !found {
		return 0, 0, fakeS3Error("InvalidRange")
	}

	start, err := strconv.ParseInt(startValue, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse fake S3 range start: %w", err)
	}
	end, err := strconv.ParseInt(endValue, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse fake S3 range end: %w", err)
	}
	if start < 0 || end < start || end >= size {
		return 0, 0, fakeS3Error("InvalidRange")
	}

	return start, end, nil
}

func fakeContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fake S3 request: %w", err)
	}

	return nil
}

func readFakeBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read fake S3 request body: %w", err)
	}

	return body, nil
}

func fakeETag(body []byte) string {
	return fmt.Sprintf("size-%d", len(body))
}

func fakeS3Error(code string) error {
	return &smithy.GenericAPIError{
		Code:    code,
		Message: code,
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}

	return aws.String(value)
}

var _ object.S3Client = (*fakeS3Client)(nil)
