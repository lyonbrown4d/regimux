package meta

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestMetadataMigrationLockCancellationDiscardsPinnedConnection(t *testing.T) {
	db, err := sql.Open("sqlite", "file:metadata-migration-lock-cancel?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	db.SetMaxOpenConns(2)
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close sqlite database: %v", closeErr)
		}
	})
	locker := &cancelingMetadataMigrationLocker{started: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	runCalled := false
	go func() {
		result <- withMetadataMigrationLock(ctx, db, locker, func() error {
			runCalled = true
			return nil
		})
	}()
	waitForMetadataMigrationLockStart(t, locker.started)
	cancel()
	assertMetadataMigrationLockCanceled(t, result)
	if runCalled {
		t.Fatal("migration ran without acquiring the lock")
	}
	if locker.unlockCalled {
		t.Fatal("unlock ran after lock acquisition was canceled")
	}
	if opened := db.Stats().OpenConnections; opened != 0 {
		t.Fatalf("open connections after canceled lock = %d, want discarded pinned connection", opened)
	}
	assertMetadataMigrationDatabaseAvailable(t, db, "after canceled lock")
}

func waitForMetadataMigrationLockStart(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("migration lock attempt did not start")
	}
}

func assertMetadataMigrationLockCanceled(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case lockErr := <-result:
		if !errors.Is(lockErr, context.Canceled) {
			t.Fatalf("migration lock error = %v, want context cancellation", lockErr)
		}
	case <-time.After(time.Second):
		t.Fatal("migration lock did not stop after context cancellation")
	}
}

func assertMetadataMigrationDatabaseAvailable(t *testing.T, db *sql.DB, stage string) {
	t.Helper()
	if pingErr := db.PingContext(t.Context()); pingErr != nil {
		t.Fatalf("database connection unavailable %s: %v", stage, pingErr)
	}
}

func TestMetadataMigrationLockUnlockFailurePreservesErrorsAndDiscardsConnection(t *testing.T) {
	db, err := sql.Open("sqlite", "file:metadata-migration-lock-unlock?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	db.SetMaxOpenConns(2)
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close sqlite database: %v", closeErr)
		}
	})
	runErr := errors.New("migration failed")
	unlockErr := errors.New("unlock failed")
	locker := &failingUnlockMetadataMigrationLocker{err: unlockErr}
	err = withMetadataMigrationLock(t.Context(), db, locker, func() error { return runErr })
	if !errors.Is(err, runErr) || !errors.Is(err, unlockErr) {
		t.Fatalf("migration error = %v, want migration and unlock errors", err)
	}
	if opened := db.Stats().OpenConnections; opened != 0 {
		t.Fatalf("open connections after unlock failure = %d, want discarded pinned connection", opened)
	}
	assertMetadataMigrationDatabaseAvailable(t, db, "after unlock failure")
}

func TestMetadataMigrationLockRejectsSingleConnectionPool(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close sqlite database: %v", closeErr)
		}
	})
	locker := &cancelingMetadataMigrationLocker{started: make(chan struct{})}
	err = withMetadataMigrationLock(t.Context(), db, locker, func() error { return nil })
	if !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("migration lock error = %v, want unsupported pool size", err)
	}
	select {
	case <-locker.started:
		t.Fatal("lock attempt started with a single connection pool")
	default:
	}
}

type cancelingMetadataMigrationLocker struct {
	started      chan struct{}
	unlockCalled bool
}

func (l *cancelingMetadataMigrationLocker) SessionLock(ctx context.Context, _ *sql.Conn) error {
	close(l.started)
	<-ctx.Done()
	return fmt.Errorf("wait for test lock: %w", ctx.Err())
}

type failingUnlockMetadataMigrationLocker struct {
	err error
}

func (failingUnlockMetadataMigrationLocker) SessionLock(context.Context, *sql.Conn) error {
	return nil
}

func (l failingUnlockMetadataMigrationLocker) SessionUnlock(context.Context, *sql.Conn) error {
	return l.err
}

func (l *cancelingMetadataMigrationLocker) SessionUnlock(context.Context, *sql.Conn) error {
	l.unlockCalled = true
	return nil
}
