package meta

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMetadataMigrationLockWaitHonorsContext(t *testing.T) {
	driver := strings.ToLower(os.Getenv("REGIMUX_META_CONCURRENCY_DRIVER"))
	dsn := os.Getenv("REGIMUX_META_CONCURRENCY_DSN")
	requireMetadataMigrationLockDatabase(t, driver, dsn)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatalf("open lock test database: %v", err)
	}
	db.SetMaxOpenConns(2)
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close lock test database: %v", closeErr)
		}
	}()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open lock holder connection: %v", err)
	}
	locker, enabled, err := metadataMigrationLocker(driver)
	if err != nil || !enabled {
		t.Fatalf("create migration lock: enabled=%t error=%v", enabled, err)
	}
	if lockErr := locker.SessionLock(ctx, conn); lockErr != nil {
		t.Fatalf("acquire migration lock: %v", lockErr)
	}
	defer func() { releaseMetadataMigrationLock(t, locker, conn) }()

	assertMetadataMigrationLockWaitCanceled(ctx, t, driver, dsn)
	if releaseErr := releaseMetadataMigrationLockError(locker, conn); releaseErr != nil {
		t.Fatalf("release migration lock after canceled waiter: %v", releaseErr)
	}
	conn = nil
	assertMetadataMigrationDBOpensAfterLockRelease(ctx, t, driver, dsn)
}

func requireMetadataMigrationLockDatabase(t *testing.T, driver, dsn string) {
	t.Helper()
	if (driver != metaDriverPostgres && driver != metaDriverMySQL) || dsn == "" {
		t.Skip("set REGIMUX_META_CONCURRENCY_DRIVER and REGIMUX_META_CONCURRENCY_DSN to a disposable database")
	}
}

func assertMetadataMigrationLockWaitCanceled(ctx context.Context, t *testing.T, driver, dsn string) {
	t.Helper()
	waitCtx, waitCancel := context.WithTimeout(ctx, time.Second)
	defer waitCancel()
	started := time.Now()
	_, openErr := OpenMetadataDB(waitCtx, DBOptions{Driver: driver, DSN: dsn})
	if !errors.Is(openErr, context.DeadlineExceeded) {
		t.Fatalf("open while migration lock is held = %v, want deadline exceeded", openErr)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("canceled migration lock wait took %s", elapsed)
	}
}

func assertMetadataMigrationDBOpensAfterLockRelease(ctx context.Context, t *testing.T, driver, dsn string) {
	t.Helper()
	opened, err := OpenMetadataDB(ctx, DBOptions{Driver: driver, DSN: dsn})
	if err != nil {
		t.Fatalf("open metadata DB after releasing lock: %v", err)
	}
	if closeErr := opened.Close(); closeErr != nil {
		t.Fatalf("close metadata DB after lock wait test: %v", closeErr)
	}
}

func releaseMetadataMigrationLock(t *testing.T, locker interface {
	SessionUnlock(context.Context, *sql.Conn) error
}, conn *sql.Conn) {
	t.Helper()
	if conn == nil {
		return
	}
	if err := releaseMetadataMigrationLockError(locker, conn); err != nil {
		t.Errorf("release migration lock during cleanup: %v", err)
	}
}

func releaseMetadataMigrationLockError(locker interface {
	SessionUnlock(context.Context, *sql.Conn) error
}, conn *sql.Conn) error {
	ctx, cancel := context.WithTimeout(context.Background(), metadataMigrationUnlockTimeout)
	defer cancel()
	unlockErr := locker.SessionUnlock(ctx, conn)
	closeErr := conn.Close()
	return joinError("release migration lock test connection", unlockErr, wrapError(closeErr, "close lock holder connection"))
}
