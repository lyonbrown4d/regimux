package object_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	ocidigest "github.com/opencontainers/go-digest"
)

type changingFileS3Client struct {
	*fakeS3Client
	file *os.File
}

func (c changingFileS3Client) PutObject(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if _, err := c.file.WriteAt([]byte("changed after validation"), 0); err != nil {
		return nil, fmt.Errorf("change caller-owned input: %w", err)
	}
	return c.fakeS3Client.PutObject(ctx, input, opts...)
}

func TestS3CallerOwnedFileIsSnapshottedBeforeUpload(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "source-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	payload := bytes.Repeat([]byte("original immutable snapshot\n"), 4096)
	if _, writeErr := file.Write(payload); writeErr != nil {
		t.Fatal(writeErr)
	}
	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		t.Fatal(seekErr)
	}
	client := changingFileS3Client{fakeS3Client: newFakeS3Client(), file: file}
	store, err := object.NewS3WithClient(client, object.S3Options{Bucket: "snapshot", Prefix: "snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	digest := ocidigest.FromBytes(payload).String()
	if _, err := store.Put(t.Context(), digest, file, object.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := verifyMemoryS3Object(t.Context(), store, digest, int64(len(payload))); err != nil {
		t.Fatal(err)
	}
}

func TestS3ImmutableFileReaderReusesCompletedSpool(t *testing.T) {
	spoolDirectory := t.TempDir()
	t.Setenv("TMPDIR", spoolDirectory)
	file, err := os.CreateTemp(t.TempDir(), "owned-source-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	payload := bytes.Repeat([]byte("owned completed cache spool\n"), 4096)
	if _, writeErr := file.Write(append([]byte("skip:"), payload...)); writeErr != nil {
		t.Fatal(writeErr)
	}
	if _, seekErr := file.Seek(5, io.SeekStart); seekErr != nil {
		t.Fatal(seekErr)
	}
	client := newBlockedMemoryS3Client(1)
	defer client.unblock()
	store, err := object.NewS3WithClient(client, object.S3Options{Bucket: "owned", Prefix: "owned"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	digest := ocidigest.FromBytes(payload).String()
	results := make(chan error, 1)
	go func() {
		_, putErr := store.Put(ctx, digest, object.ImmutableFileReader(file), object.PutOptions{})
		results <- putErr
	}()
	waitForMemoryUploadEntries(ctx, t, client.entered, 1)
	files, err := os.ReadDir(spoolDirectory)
	if err != nil || len(files) != 0 {
		t.Fatalf("owned file allocated another spool: files=%v error=%v", files, err)
	}
	client.unblock()
	waitForMemoryUploadResults(ctx, t, results, 1)
	if err := verifyMemoryS3Object(ctx, store, digest, int64(len(payload))); err != nil {
		t.Fatal(err)
	}
}
