package object_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	ocidigest "github.com/opencontainers/go-digest"
)

func TestS3MultipartErrorAbortsAndReleasesSpoolAndAdmission(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	failure := errors.New("synthetic part failure")
	client := &failingMemoryS3Client{fakeS3Client: newFakeS3Client(), failure: failure}
	client.fail.Store(true)
	store, err := object.NewS3WithClient(client, object.S3Options{
		Bucket: "failed-stream", Prefix: "failed", PartSize: 5 << 20, MaxConcurrentUploads: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	const size = 6 << 20
	digester := ocidigest.Canonical.Digester()
	if _, hashErr := io.Copy(digester.Hash(), io.LimitReader(repeatedS3Bytes{value: 7}, size)); hashErr != nil {
		t.Fatal(hashErr)
	}
	digest := digester.Digest().String()
	_, err = store.Put(t.Context(), digest, io.LimitReader(repeatedS3Bytes{value: 7}, size), object.PutOptions{})
	if !errors.Is(err, failure) {
		t.Fatalf("multipart failure = %v, want synthetic part failure", err)
	}
	assertFailedS3StreamClean(t, client.fakeS3Client, tmp, digest)
	client.fail.Store(false)
	if _, err := store.Put(t.Context(), digest, io.LimitReader(repeatedS3Bytes{value: 7}, size), object.PutOptions{}); err != nil {
		t.Fatalf("retry after failed upload: %v", err)
	}
	if err := verifyMemoryS3Object(t.Context(), store, digest, size); err != nil {
		t.Fatal(err)
	}
	assertS3StreamFilesRemoved(t, tmp)
}

func assertFailedS3StreamClean(t *testing.T, client *fakeS3Client, tmp, digest string) {
	t.Helper()
	if _, found := client.object(testS3Key("failed", digest)); found {
		t.Fatal("failed multipart upload published an object")
	}
	if got := client.uploadCount(); got != 0 {
		t.Fatalf("failed multipart upload retained %d sessions", got)
	}
	assertS3StreamFilesRemoved(t, tmp)
}

func assertS3StreamFilesRemoved(t *testing.T, tmp string) {
	t.Helper()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("upload retained %d temporary files", len(entries))
	}
}

type failingMemoryS3Client struct {
	*fakeS3Client
	failure error
	fail    atomic.Bool
}

func (client *failingMemoryS3Client) UploadPart(
	ctx context.Context,
	input *s3.UploadPartInput,
	opts ...func(*s3.Options),
) (*s3.UploadPartOutput, error) {
	if client.fail.Load() {
		return nil, fmt.Errorf("write synthetic S3 part: %w", client.failure)
	}
	return client.fakeS3Client.UploadPart(ctx, input, opts...)
}
