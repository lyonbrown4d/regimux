package object_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	ocidigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

type failedS3GetClient struct {
	*fakeS3Client
	body *failedS3GetBody
	err  error
}

func (c *failedS3GetClient) HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return &s3.HeadObjectOutput{ContentLength: aws.Int64(7)}, nil
}

func (c *failedS3GetClient) GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return &s3.GetObjectOutput{Body: c.body, ContentLength: aws.Int64(7)}, c.err
}

type failedS3GetBody struct {
	io.Reader
	closes int
	err    error
}

func (b *failedS3GetBody) Close() error {
	b.closes++
	return b.err
}

func TestS3GetErrorClosesUnexpectedResponseBody(t *testing.T) {
	getErr := errors.New("get response failed")
	closeErr := errors.New("close response failed")
	body := &failedS3GetBody{Reader: strings.NewReader("payload"), err: closeErr}
	client := &failedS3GetClient{fakeS3Client: newFakeS3Client(), body: body, err: getErr}
	store, err := object.NewS3WithClient(client, object.S3Options{Bucket: "bucket"})
	require.NoError(t, err)
	reader, info, err := store.Get(t.Context(), ocidigest.FromString("payload").String(), object.GetOptions{})
	require.ErrorIs(t, err, getErr)
	require.Nil(t, reader)
	require.Nil(t, info)
	require.Equal(t, 1, body.closes)
	require.ErrorIs(t, err, closeErr)
}
