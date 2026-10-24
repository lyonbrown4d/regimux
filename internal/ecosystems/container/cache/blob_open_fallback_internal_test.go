package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocidigest "github.com/opencontainers/go-digest"
	"golang.org/x/sync/semaphore"
)

func TestBlobOpenFallbackReaderKeepsTerminalResult(t *testing.T) {
	body := []byte("fallback body")
	wrongDigest := ocidigest.FromBytes([]byte("different body")).String()
	underlying := &countingBlobReadCloser{Reader: bytes.NewReader(body)}
	reader, err := newBlobOpenFallbackReader(context.Background(), underlying, wrongDigest, int64(len(body)), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, firstErr := io.ReadAll(reader)
	digestErr, ok := errors.AsType[*distribution.ErrorList](firstErr)
	if !ok || len(digestErr.Errors) != 1 || digestErr.Errors[0].Code != distribution.CodeDigestInvalid {
		t.Fatalf("first read error=%v, want digest error", firstErr)
	}
	readsAfterFailure := underlying.reads.Load()
	var buffer [1]byte
	_, secondErr := reader.Read(buffer[:])
	if secondErr == nil || secondErr.Error() != firstErr.Error() {
		t.Fatalf("second read error=%v, want stable first error %v", secondErr, firstErr)
	}
	if got := underlying.reads.Load(); got != readsAfterFailure {
		t.Fatalf("underlying reads after terminal digest error=%d, want %d", got, readsAfterFailure)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if got := underlying.closes.Load(); got != 1 {
		t.Fatalf("underlying closes=%d, want one", got)
	}
}

func TestBlobOpenFallbackReaderReleasesPermitOnTermination(t *testing.T) {
	for _, termination := range []string{"EOF", "Close", "cancel"} {
		t.Run(termination, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			underlying := &countingBlobReadCloser{Reader: bytes.NewReader([]byte("verified"))}
			released := make(chan struct{})
			reader, err := newBlobOpenFallbackReader(ctx, underlying, ocidigest.FromString("verified").String(), 8, true,
				func() { close(released) })
			if err != nil {
				t.Fatal(err)
			}
			terminateFallbackReader(t, termination, reader, cancel)
			waitForFallbackPermitRelease(t, released)
			assertFallbackBodyClosedOnce(t, reader, underlying)
		})
	}
}

func terminateFallbackReader(t *testing.T, termination string, reader *blobOpenFallbackReader, cancel context.CancelFunc) {
	t.Helper()
	switch termination {
	case "EOF":
		if _, err := io.ReadAll(reader); err != nil {
			t.Fatal(err)
		}
	case "Close":
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
	case "cancel":
		cancel()
	}
}

func waitForFallbackPermitRelease(t *testing.T, released <-chan struct{}) {
	t.Helper()
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("fallback permit was retained after termination")
	}
}

func assertFallbackBodyClosedOnce(t *testing.T, reader *blobOpenFallbackReader, underlying *countingBlobReadCloser) {
	t.Helper()
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if got := underlying.closes.Load(); got != 1 {
		t.Fatalf("underlying closes=%d, want one", got)
	}
}

func TestBlobOpenFallbackReaderKeepsEOF(t *testing.T) {
	body := []byte("verified fallback")
	digest := ocidigest.FromBytes(body).String()
	underlying := &countingBlobReadCloser{Reader: bytes.NewReader(body)}
	reader, err := newBlobOpenFallbackReader(context.Background(), underlying, digest, int64(len(body)), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := io.ReadAll(reader); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("read body=%q error=%v", got, err)
	}
	readsAtEOF := underlying.reads.Load()
	var buffer [1]byte
	if n, err := reader.Read(buffer[:]); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("read after EOF=(%d,%v), want (0, EOF)", n, err)
	}
	if got := underlying.reads.Load(); got != readsAtEOF {
		t.Fatalf("underlying reads after EOF=%d, want %d", got, readsAtEOF)
	}
}

func TestBlobOpenFallbackGateLimitsRequestsAndCancelsWaiter(t *testing.T) {
	body := []byte("gated fallback body")
	digest := ocidigest.FromBytes(body).String()
	client := &gatedBlobClient{body: body, started: make(chan struct{}, 5), release: make(chan struct{})}
	t.Cleanup(client.releaseAll)
	proxy := blobProxy{client: client, openFallbackGate: semaphore.NewWeighted(blobOpenFallbackConcurrency)}
	req := BlobRequest{UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest, Method: http.MethodGet}
	results := make(chan *BlobReadResult, 4)
	errorsCh := make(chan error, 5)
	for range 4 {
		go func() {
			result, err := proxy.fallbackAfterStoredObjectOpenError(context.Background(), req, nil, newStoredBlobOpenError(errors.New("injected")))
			results <- result
			errorsCh <- err
		}()
	}
	waitForBlobFallbackStarts(t, client.started, 4)
	assertFallbackWaiterCanCancel(t, proxy, req)
	if got := client.calls.Load(); got != 4 {
		t.Fatalf("upstream calls before releasing gate=%d, want four", got)
	}

	client.releaseAll()
	for range 4 {
		result := <-results
		err := <-errorsCh
		if err != nil {
			t.Fatalf("fallback error=%v", err)
		}
		if closeErr := result.Reader.Close(); closeErr != nil {
			t.Fatalf("close fallback reader: %v", closeErr)
		}
	}
	assertAllFallbackPermitsReleased(t, proxy.openFallbackGate)
	if got := client.calls.Load(); got != 4 {
		t.Fatalf("upstream calls after releasing gate=%d, want four", got)
	}
	if got := int64(client.maximum.Load()); got != blobOpenFallbackConcurrency {
		t.Fatalf("max active upstream fallbacks=%d, want %d", got, blobOpenFallbackConcurrency)
	}
}

func assertFallbackWaiterCanCancel(t *testing.T, proxy blobProxy, req BlobRequest) {
	t.Helper()
	baseCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waitingCtx := &observeDoneContext{Context: baseCtx, observed: make(chan struct{})}
	waiterDone := make(chan error, 1)
	go func() {
		_, err := proxy.fallbackAfterStoredObjectOpenError(waitingCtx, req, nil, newStoredBlobOpenError(errors.New("injected")))
		waiterDone <- err
	}()
	select {
	case <-waitingCtx.observed:
	case <-time.After(5 * time.Second):
		t.Fatal("fallback waiter did not reach the gate")
	}
	cancel()
	select {
	case err := <-waiterDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled gate waiter error=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled fallback waiter did not return")
	}
}

func assertAllFallbackPermitsReleased(t *testing.T, gate *semaphore.Weighted) {
	t.Helper()
	for range blobOpenFallbackConcurrency {
		if !gate.TryAcquire(1) {
			t.Fatal("fallback reader retained a gate permit after Close")
		}
	}
	gate.Release(blobOpenFallbackConcurrency)
}

func waitForBlobFallbackStarts(t *testing.T, started <-chan struct{}, count int) {
	t.Helper()
	for range count {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("fallback did not reach upstream")
		}
	}
}

type observeDoneContext struct {
	context.Context
	observed chan struct{}
	once     atomic.Bool
}

func (c *observeDoneContext) Done() <-chan struct{} {
	if c.once.CompareAndSwap(false, true) {
		close(c.observed)
	}
	return c.Context.Done()
}

type gatedBlobClient struct {
	body        []byte
	started     chan struct{}
	release     chan struct{}
	calls       atomic.Int32
	active      atomic.Int32
	maximum     atomic.Int32
	releaseOnce sync.Once
}

func (c *gatedBlobClient) releaseAll() {
	c.releaseOnce.Do(func() { close(c.release) })
}
func (c *gatedBlobClient) GetBlob(ctx context.Context, _ upstream.GetBlobRequest) (*upstream.BlobResponse, error) {
	c.calls.Add(1)
	active := c.active.Add(1)
	for {
		maximum := c.maximum.Load()
		if active <= maximum || c.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	c.started <- struct{}{}
	select {
	case <-c.release:
	case <-ctx.Done():
		c.active.Add(-1)
		return nil, fmt.Errorf("wait for gated upstream response: %w", ctx.Err())
	}
	return &upstream.BlobResponse{
		Body:       &gatedBlobBody{Reader: bytes.NewReader(c.body), onClose: func() { c.active.Add(-1) }},
		Digest:     ocidigest.FromBytes(c.body).String(),
		Size:       int64(len(c.body)),
		StatusCode: http.StatusOK,
		Headers:    http.Header{distribution.HeaderContentLength: []string{strconv.Itoa(len(c.body))}},
	}, nil
}
func (*gatedBlobClient) ConsumeBlob(context.Context, upstream.GetBlobRequest, upstream.BlobConsumeFunc) error {
	return errors.New("unexpected ConsumeBlob")
}

type gatedBlobBody struct {
	*bytes.Reader
	onClose func()
	once    atomic.Bool
}

func (b *gatedBlobBody) Close() error {
	if b.once.CompareAndSwap(false, true) {
		b.onClose()
	}
	return nil
}

type countingBlobReadCloser struct {
	*bytes.Reader
	reads  atomic.Int32
	closes atomic.Int32
}

func (r *countingBlobReadCloser) Read(buffer []byte) (int, error) {
	r.reads.Add(1)
	n, err := r.Reader.Read(buffer)
	if err != nil {
		return n, fmt.Errorf("read counting body: %w", err)
	}
	return n, nil
}
func (r *countingBlobReadCloser) Close() error {
	r.closes.Add(1)
	return nil
}
