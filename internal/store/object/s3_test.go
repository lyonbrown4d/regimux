package object_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"github.com/lyonbrown4d/regimux/internal/store/object"
)

const testS3PartSize int64 = 5 * 1024 * 1024

type s3Fixture struct {
	store   *object.S3Store
	client  *fakeS3Client
	payload []byte
	digest  string
	key     string
}

func TestS3StoreContract(t *testing.T) {
	fixture := newS3Fixture(t)

	t.Run("put and stat", fixture.testPutAndStat)
	t.Run("get and range", fixture.testGet)
	t.Run("walk and list", fixture.testWalkAndList)
	t.Run("delete", fixture.testDelete)
}

func newS3Fixture(t *testing.T) *s3Fixture {
	t.Helper()

	client := newFakeS3Client()
	store, err := object.NewS3WithClient(client, object.S3Options{
		Bucket: "bucket",
		Prefix: "tenant/cache",
	})
	require.NoError(t, err)

	payload := []byte("0123456789")
	digest := godigest.FromBytes(payload).String()

	return &s3Fixture{
		store:   store,
		client:  client,
		payload: payload,
		digest:  digest,
		key:     testS3Key("tenant/cache", digest),
	}
}

func (f *s3Fixture) testPutAndStat(t *testing.T) {
	info, err := f.store.Put(
		t.Context(),
		f.digest,
		bytes.NewReader(f.payload),
		object.PutOptions{
			ContentType: "application/octet-stream",
			Metadata:    map[string]string{"source": "contract-test"},
		},
	)
	require.NoError(t, err)
	require.Equal(t, int64(len(f.payload)), info.Size)
	require.Equal(t, "application/octet-stream", info.ContentType)

	stored, found := f.client.object(f.key)
	require.True(t, found)
	require.Equal(t, f.payload, stored.body)
	require.Equal(t, "application/octet-stream", stored.contentType)
	require.Equal(t, "contract-test", stored.metadata["source"])

	stat, err := f.store.Stat(t.Context(), f.digest)
	require.NoError(t, err)
	require.Equal(t, int64(len(f.payload)), stat.Size)
	require.Equal(t, "application/octet-stream", stat.ContentType)

	exists, err := f.store.Exists(t.Context(), f.digest)
	require.NoError(t, err)
	require.True(t, exists)
}

func (f *s3Fixture) testGet(t *testing.T) {
	body, fullInfo, err := f.store.Get(t.Context(), f.digest, object.GetOptions{})
	require.NoError(t, err)
	fullPayload, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	require.Equal(t, f.payload, fullPayload)
	require.Equal(t, int64(len(f.payload)), fullInfo.Size)

	body, rangeInfo, err := f.store.Get(t.Context(), f.digest, object.GetOptions{
		Range: &object.HTTPRange{Start: 2, End: 5},
	})
	require.NoError(t, err)
	rangePayload, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	require.Equal(t, []byte("2345"), rangePayload)
	require.Equal(t, int64(4), rangeInfo.Size)
}

func (f *s3Fixture) testWalkAndList(t *testing.T) {
	walked := make([]object.Info, 0)
	err := f.store.WalkObjects(t.Context(), func(info object.Info) error {
		walked = append(walked, info)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, walked, 1)
	require.Equal(t, f.digest, walked[0].Digest)

	listed, err := f.store.ListObjects(t.Context())
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, f.digest, listed[0].Digest)
}

func (f *s3Fixture) testDelete(t *testing.T) {
	require.NoError(t, f.store.Delete(t.Context(), f.digest))

	exists, err := f.store.Exists(t.Context(), f.digest)
	require.NoError(t, err)
	require.False(t, exists)
	require.NoError(t, f.store.Delete(t.Context(), f.digest))
}

func TestS3StoreMultipartUploadIsDigestAtomic(t *testing.T) {
	client := newFakeS3Client()
	store, err := object.NewS3WithClient(client, object.S3Options{
		Bucket:            "bucket",
		Prefix:            "multipart",
		PartSize:          testS3PartSize,
		UploadConcurrency: 2,
	})
	require.NoError(t, err)

	payload := bytes.Repeat([]byte("x"), int(testS3PartSize)+257)
	digest := godigest.FromBytes(payload).String()
	_, err = store.Put(t.Context(), digest, bytes.NewReader(payload), object.PutOptions{})
	require.NoError(t, err)

	stored, found := client.object(testS3Key("multipart", digest))
	require.True(t, found)
	require.Equal(t, payload, stored.body)
	require.Zero(t, client.uploadCount())

	wrongDigest := godigest.FromString("different payload").String()
	_, err = store.Put(t.Context(), wrongDigest, bytes.NewReader(payload), object.PutOptions{})
	require.ErrorIs(t, err, object.ErrDigestMismatch)

	_, found = client.object(testS3Key("multipart", wrongDigest))
	require.False(t, found)
	require.Zero(t, client.uploadCount())
}

func TestS3StorePropagatesContextCancellation(t *testing.T) {
	client := newFakeS3Client()
	store, err := object.NewS3WithClient(client, object.S3Options{Bucket: "bucket"})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = store.Stat(ctx, godigest.FromString("missing").String())
	require.ErrorIs(t, err, context.Canceled)
}

func TestNewS3StoreRejectsInvalidUploadTuning(t *testing.T) {
	client := newFakeS3Client()

	_, err := object.NewS3WithClient(client, object.S3Options{
		Bucket:   "bucket",
		PartSize: testS3PartSize - 1,
	})
	require.Error(t, err)

	_, err = object.NewS3WithClient(client, object.S3Options{
		Bucket:            "bucket",
		UploadConcurrency: -1,
	})
	require.Error(t, err)
}

func testS3Key(prefix, digest string) string {
	algorithm, encoded, _ := strings.Cut(digest, ":")
	return prefix + "/blobs/" + algorithm + "/" + encoded[:2] + "/" + encoded
}
