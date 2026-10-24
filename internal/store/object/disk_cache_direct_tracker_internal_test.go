package object

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiskCacheDirectTrackerCloseClosesUnconsumedBody(t *testing.T) {
	body := newTrackerTestBody()
	rng := &HTTPRange{Start: 2, End: 8}
	info := &Info{Digest: "sha256:test", Size: 7}
	backend := &trackerTestStore{body: body, info: info}
	tracker := newDiskCacheDirectTracker()
	reader, gotInfo, err := tracker.open(context.Background(), backend, "sha256:test", GetOptions{Range: rng})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if backend.gotOpts.Range != rng {
		t.Fatal("backend Get did not receive the original Range option")
	}
	if gotInfo != info {
		t.Fatal("open did not preserve backend Info")
	}
	if err := tracker.close(); err != nil {
		t.Fatalf("close tracker: %v", err)
	}
	if got := body.closeCount.Load(); got != 1 {
		t.Fatalf("body Close calls = %d, want 1", got)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close returned reader: %v", err)
	}
	if got := body.closeCount.Load(); got != 1 {
		t.Fatalf("body Close calls after reader Close = %d, want 1", got)
	}
}

func TestDiskCacheDirectTrackerCallerCancelClosesBody(t *testing.T) {
	body := newTrackerTestBody()
	tracker := newDiskCacheDirectTracker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, _, err := tracker.open(ctx, &trackerTestStore{body: body}, "sha256:test", GetOptions{}); err != nil {
		t.Fatalf("open: %v", err)
	}
	cancel()
	waitForTrackerSignal(t, body.closed, "body close after caller cancellation")
	if got := body.closeCount.Load(); got != 1 {
		t.Fatalf("body Close calls = %d, want 1", got)
	}
	if err := tracker.close(); err != nil {
		t.Fatalf("close tracker: %v", err)
	}
}

func TestDiskCacheDirectTrackerCloseWaitsForCanceledPendingGet(t *testing.T) {
	body := newTrackerTestBody()
	backend := &trackerPendingStore{
		body:     body,
		started:  make(chan struct{}),
		canceled: make(chan struct{}),
		release:  make(chan struct{}),
	}
	tracker := newDiskCacheDirectTracker()
	callerCtx, cancelCaller := context.WithCancel(context.Background())
	t.Cleanup(cancelCaller)
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(backend.release) }) })
	openResult := make(chan error, 1)
	go func() {
		reader, _, err := tracker.open(callerCtx, backend, "sha256:test", GetOptions{})
		if reader != nil {
			err = errors.Join(err, reader.Close())
		}
		openResult <- err
	}()
	waitForTrackerSignal(t, backend.started, "backend Get start")

	closeResult := make(chan error, 1)
	go func() { closeResult <- tracker.close() }()
	waitForTrackerSignal(t, backend.canceled, "pending Get cancellation")
	assertTrackerClosePending(t, closeResult)
	releaseOnce.Do(func() { close(backend.release) })

	if err := waitForTrackerError(t, openResult, "open after pending Get"); err == nil {
		t.Fatal("open returned nil error after tracker closed a pending Get")
	}
	if err := waitForTrackerError(t, closeResult, "tracker close"); err != nil {
		t.Fatalf("close tracker: %v", err)
	}
	if got := body.closeCount.Load(); got != 1 {
		t.Fatalf("late body Close calls = %d, want 1", got)
	}
}

func TestDiskCacheDirectTrackerConcurrentCancelAndCloseIsIdempotent(t *testing.T) {
	const readers = 24
	tracker := newDiskCacheDirectTracker()
	bodies := make([]*trackerTestBody, readers)
	cancels := make([]context.CancelFunc, readers)
	for i := range readers {
		bodies[i] = newTrackerTestBody()
		ctx, cancel := context.WithCancel(context.Background())
		cancels[i] = cancel
		if _, _, err := tracker.open(ctx, &trackerTestStore{body: bodies[i]}, "sha256:test", GetOptions{}); err != nil {
			t.Fatalf("open reader %d: %v", i, err)
		}
	}

	cancelTrackerReaders(cancels)
	closeErrors := make(chan error, 2)
	for range 2 {
		go func() { closeErrors <- tracker.close() }()
	}
	assertTrackerBodiesClosed(t, bodies)
	for range 2 {
		if err := waitForTrackerError(t, closeErrors, "concurrent tracker close"); err != nil {
			t.Fatalf("close tracker: %v", err)
		}
	}
}

func TestDiskCacheDirectTrackerRetainsCallerCancelCloseError(t *testing.T) {
	sentinel := errors.New("body close failed")
	body := newTrackerTestBody()
	body.closeErr = sentinel
	tracker := newDiskCacheDirectTracker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, _, err := tracker.open(ctx, &trackerTestStore{body: body}, "sha256:test", GetOptions{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	cancel()
	waitForTrackerSignal(t, body.closed, "body close after caller cancellation")
	directReader, ok := reader.(*diskCacheDirectReader)
	if !ok {
		t.Fatalf("reader type = %T, want *diskCacheDirectReader", reader)
	}
	waitForTrackerSignal(t, directReader.request.callbackDone, "cancellation callback completion")

	if err := reader.Close(); !errors.Is(err, sentinel) {
		t.Fatalf("reader Close error = %v, want sentinel", err)
	}
	if err := tracker.close(); !errors.Is(err, sentinel) {
		t.Fatalf("tracker Close error = %v, want sentinel", err)
	}
}

func TestDiskCacheDirectTrackerConcurrentCloseWaitersShareError(t *testing.T) {
	sentinel := errors.New("body close failed")
	body := newTrackerTestBody()
	body.closeErr = sentinel
	tracker := newDiskCacheDirectTracker()
	if _, _, err := tracker.open(context.Background(), &trackerTestStore{body: body}, "sha256:test", GetOptions{}); err != nil {
		t.Fatalf("open: %v", err)
	}

	const callers = 2
	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			results <- tracker.close()
		}()
	}
	close(start)
	for range callers {
		if err := waitForTrackerError(t, results, "concurrent tracker close"); !errors.Is(err, sentinel) {
			t.Fatalf("concurrent tracker Close error = %v, want sentinel", err)
		}
	}
}

type trackerTestStore struct {
	body    *trackerTestBody
	info    *Info
	gotOpts GetOptions
}

var errTrackerTestUnsupported = errors.New("tracker test operation unsupported")

func (s *trackerTestStore) Stat(context.Context, string) (*Info, error) {
	return nil, errTrackerTestUnsupported
}
func (s *trackerTestStore) Exists(context.Context, string) (bool, error) { return false, nil }
func (s *trackerTestStore) Put(context.Context, string, io.Reader, PutOptions) (*Info, error) {
	return nil, errTrackerTestUnsupported
}
func (s *trackerTestStore) Delete(context.Context, string) error { return nil }
func (s *trackerTestStore) Get(_ context.Context, _ string, opts GetOptions) (io.ReadCloser, *Info, error) {
	s.gotOpts = opts
	info := s.info
	if info == nil {
		info = &Info{Digest: "sha256:test", Size: 1}
	}
	return s.body, info, nil
}

type trackerPendingStore struct {
	body     *trackerTestBody
	started  chan struct{}
	canceled chan struct{}
	release  chan struct{}
}

func (s *trackerPendingStore) Stat(context.Context, string) (*Info, error) {
	return nil, errTrackerTestUnsupported
}
func (s *trackerPendingStore) Exists(context.Context, string) (bool, error) { return false, nil }
func (s *trackerPendingStore) Put(context.Context, string, io.Reader, PutOptions) (*Info, error) {
	return nil, errTrackerTestUnsupported
}
func (s *trackerPendingStore) Delete(context.Context, string) error { return nil }
func (s *trackerPendingStore) Get(ctx context.Context, _ string, _ GetOptions) (io.ReadCloser, *Info, error) {
	close(s.started)
	<-ctx.Done()
	close(s.canceled)
	<-s.release
	return s.body, &Info{Digest: "sha256:test", Size: 1}, nil
}

type trackerTestBody struct {
	closeCount atomic.Int32
	closed     chan struct{}
	closeErr   error
	once       sync.Once
}

func newTrackerTestBody() *trackerTestBody {
	return &trackerTestBody{closed: make(chan struct{})}
}

func (b *trackerTestBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *trackerTestBody) Close() error {
	b.closeCount.Add(1)
	b.once.Do(func() { close(b.closed) })
	return b.closeErr
}

func cancelTrackerReaders(cancels []context.CancelFunc) {
	var done sync.WaitGroup
	for _, cancel := range cancels {
		done.Add(1)
		go func(cancel context.CancelFunc) {
			defer done.Done()
			cancel()
		}(cancel)
	}
	done.Wait()
}

func assertTrackerBodiesClosed(t *testing.T, bodies []*trackerTestBody) {
	t.Helper()
	for i, body := range bodies {
		waitForTrackerSignal(t, body.closed, "concurrent body close")
		if got := body.closeCount.Load(); got != 1 {
			t.Fatalf("body %d Close calls = %d, want 1", i, got)
		}
	}
}

func waitForTrackerSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	if signal == nil {
		t.Fatalf("%s signal was not initialized", label)
	}
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func assertTrackerClosePending(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("tracker Close returned before pending Get exited: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
}

func waitForTrackerError(t *testing.T, result <-chan error, label string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
		return errors.New("unreachable")
	}
}
