package object

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	streamlib "github.com/djherbis/stream"
	ocidigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

type diskCacheGatedVerifier struct {
	verifier ocidigest.Verifier
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
}

type diskCacheFinalChunkCleanup struct {
	t              *testing.T
	cache          *DiskCacheStore
	stream         *streamlib.Stream
	reader         *streamlib.Reader
	path           string
	producerDone   <-chan error
	release        func()
	producerWaited bool
	readerClosed   bool
	streamCanceled bool
}

func TestDiskCacheFinalBytesWaitForDigestVerification(t *testing.T) {
	runDiskCacheFinalChunkVerification(t)
}

type diskCacheFinalChunkCase struct {
	payload      []byte
	prefix       []byte
	fill         *diskCacheFill
	verifier     *diskCacheGatedVerifier
	reader       *streamlib.Reader
	producerDone chan error
	cleanup      *diskCacheFinalChunkCleanup
}

func newDiskCacheFinalChunkCase(t *testing.T) *diskCacheFinalChunkCase {
	t.Helper()
	backend, _, _ := newDiskCacheTestBackend(t)
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))
	payload := bytes.Repeat([]byte("x"), 64*1024)
	digest := digestForTest(payload)
	parsed, err := ocidigest.Parse(digest)
	require.NoError(t, err, "parse test digest")
	verifier := &diskCacheGatedVerifier{
		verifier: parsed.Verifier(),
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	path, err := cache.newDiskCacheTempPath("verify-")
	require.NoError(t, err, "allocate stream path")
	stream, err := streamlib.NewStream(path, &diskCacheStreamFS{store: cache, name: path})
	require.NoError(t, err, "create shared stream")
	require.NoError(t, stream.SetSeekEnd(int64(len(payload))), "set stream size")
	reader, err := stream.NextReader()
	require.NoError(t, err, "open consumer stream reader")

	fill := &diskCacheFill{
		ctx:    t.Context(),
		info:   Info{Digest: digest, Size: int64(len(payload))},
		source: io.NopCloser(bytes.NewReader(payload)),
		stream: stream,
	}
	gateRelease := sync.OnceFunc(func() { close(verifier.release) })
	producerDone := make(chan error, 1)
	cleanup := &diskCacheFinalChunkCleanup{
		t: t, cache: cache, stream: stream, reader: reader, path: path,
		producerDone: producerDone, release: gateRelease,
	}
	t.Cleanup(cleanup.run)
	return &diskCacheFinalChunkCase{
		payload: payload, prefix: make([]byte, 32*1024), verifier: verifier,
		fill: fill, reader: reader, producerDone: producerDone, cleanup: cleanup,
	}
}

func runDiskCacheFinalChunkVerification(t *testing.T) {
	t.Helper()
	testCase := newDiskCacheFinalChunkCase(t)
	go runDiskCacheVerificationCopy(testCase.fill, testCase.verifier, testCase.producerDone)
	if _, err := io.ReadFull(testCase.reader, testCase.prefix); err != nil {
		t.Fatalf("read stream prefix: %v", err)
	}
	require.Equal(t, testCase.payload[:len(testCase.prefix)], testCase.prefix, "verified prefix remains streamable")
	remaining := make(chan diskCacheVerifiedRead, 1)
	go func() {
		var body bytes.Buffer
		_, readErr := io.CopyN(&body, testCase.reader, int64(len(testCase.payload)-len(testCase.prefix)))
		remaining <- diskCacheVerifiedRead{body: body.Bytes(), err: readErr}
	}()
	waitForDiskCacheSignal(t, testCase.verifier.entered)

	early := readBeforeDiskCacheGateRelease(remaining)
	testCase.cleanup.release()
	copyErr := <-testCase.producerDone
	testCase.cleanup.producerWaited = true
	require.ErrorIs(t, copyErr, ErrDigestMismatch, "failed final digest verification")
	final := awaitDiskCacheFinalChunkRead(t, remaining, early)
	if len(final.body) == len(testCase.payload)-len(testCase.prefix) && final.err == nil {
		t.Error("consumer received the unverified final bytes")
	}
	if final.err != nil && !errors.Is(final.err, ErrDigestMismatch) {
		t.Errorf("consumer read error = %v, want digest mismatch", final.err)
	}
	if err := testCase.reader.Close(); err != nil {
		t.Errorf("close consumer stream reader: %v", err)
	}
	testCase.cleanup.readerClosed = true
	testCase.cleanup.streamCanceled = true
}

func readBeforeDiskCacheGateRelease(remaining <-chan diskCacheVerifiedRead) *diskCacheVerifiedRead {
	select {
	case result := <-remaining:
		return &result
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func awaitDiskCacheFinalChunkRead(
	t *testing.T,
	remaining <-chan diskCacheVerifiedRead,
	early *diskCacheVerifiedRead,
) diskCacheVerifiedRead {
	t.Helper()
	if early != nil {
		return *early
	}
	select {
	case result := <-remaining:
		return result
	case <-time.After(time.Second):
		t.Fatal("consumer did not stop after digest verification failed")
		return diskCacheVerifiedRead{}
	}
}

func (c *diskCacheFinalChunkCleanup) run() {
	c.release()
	c.waitForProducer()
	c.closeReaderIfNeeded()
	c.cancelStreamIfNeeded()
	c.removeFile()
}

func (c *diskCacheFinalChunkCleanup) waitForProducer() {
	if !c.producerWaited {
		select {
		case err := <-c.producerDone:
			if err != nil && !errors.Is(err, ErrDigestMismatch) {
				c.t.Errorf("finish gated producer: %v", err)
			}
		case <-time.After(time.Second):
			c.t.Error("gated producer did not stop during cleanup")
		}
	}
}

func (c *diskCacheFinalChunkCleanup) closeReaderIfNeeded() {
	if !c.readerClosed {
		if err := c.reader.Close(); err != nil {
			c.t.Errorf("close consumer stream reader: %v", err)
		}
	}
}

func (c *diskCacheFinalChunkCleanup) cancelStreamIfNeeded() {
	if !c.streamCanceled {
		if err := c.stream.CancelWithErr(context.Canceled); err != nil {
			c.t.Errorf("cancel shared stream: %v", err)
		}
	}
}

func (c *diskCacheFinalChunkCleanup) removeFile() {
	if err := c.cache.removeCacheFile(c.path); err != nil {
		c.t.Errorf("remove test stream file: %v", err)
	}
}

type diskCacheVerifiedRead struct {
	body []byte
	err  error
}

func runDiskCacheVerificationCopy(
	fill *diskCacheFill,
	verifier *diskCacheGatedVerifier,
	result chan<- error,
) {
	err := copyVerifiedDiskCacheStream(fill, verifier)
	closeErr := fill.source.Close()
	if err == nil {
		err = errors.Join(closeErr, fill.stream.Close())
	} else {
		cause := err
		err = errors.Join(cause, closeErr, fill.stream.CancelWithErr(cause))
	}
	result <- err
}

func (v *diskCacheGatedVerifier) Write(data []byte) (int, error) {
	n, err := v.verifier.Write(data)
	if err != nil {
		return n, fmt.Errorf("write gated digest verifier: %w", err)
	}
	return n, nil
}

func (v *diskCacheGatedVerifier) Verified() bool {
	v.once.Do(func() {
		close(v.entered)
		<-v.release
	})
	return false
}
