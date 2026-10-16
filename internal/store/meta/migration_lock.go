package meta

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"time"

	gooselock "github.com/pressly/goose/v3/lock"
)

const (
	metadataMigrationPostgresLockID   int64 = 0x524547494D55584D
	metadataMigrationMySQLWaitSeconds       = 300
	metadataMigrationUnlockTimeout          = 5 * time.Second
)

type mysqlMetadataMigrationLocker struct{}

func metadataMigrationLocker(dbDriver string) (gooselock.SessionLocker, bool, error) {
	switch normalizeDBDriver(dbDriver) {
	case metaDriverPostgres:
		locker, err := gooselock.NewPostgresSessionLocker(
			gooselock.WithLockID(metadataMigrationPostgresLockID),
		)
		if err != nil {
			return nil, false, wrapError(err, "create postgres metadata migration lock")
		}
		return locker, true, nil
	case metaDriverMySQL:
		return mysqlMetadataMigrationLocker{}, true, nil
	default:
		return nil, false, nil
	}
}

func withMetadataMigrationLock(
	ctx context.Context,
	db *sql.DB,
	locker gooselock.SessionLocker,
	run func() error,
) (resultErr error) {
	if db.Stats().MaxOpenConnections == 1 {
		return errorf("%w: metadata migrations require at least two open database connections", ErrInvalidValue)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return wrapError(err, "open metadata migration lock connection")
	}
	locked := false
	discard := false
	defer func() {
		resultErr = finishMetadataMigrationLock(ctx, conn, locker, locked, discard, resultErr)
	}()
	if err := locker.SessionLock(ctx, conn); err != nil {
		discard = true
		return wrapError(err, "acquire metadata migration lock")
	}
	locked = true
	return run()
}

func finishMetadataMigrationLock(
	ctx context.Context,
	conn *sql.Conn,
	locker gooselock.SessionLocker,
	locked bool,
	discard bool,
	resultErr error,
) error {
	if locked {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), metadataMigrationUnlockTimeout)
		unlockErr := locker.SessionUnlock(unlockCtx, conn)
		cancel()
		if unlockErr != nil {
			discard = true
			resultErr = joinError("run metadata migrations and release lock", resultErr,
				wrapError(unlockErr, "release metadata migration lock"))
		}
	}
	if discard {
		resultErr = joinError("discard metadata migration lock connection", resultErr,
			discardMetadataMigrationConn(conn))
	}
	if closeErr := closeMetadataMigrationConn(conn); closeErr != nil {
		resultErr = joinError("close metadata migration lock connection", resultErr, closeErr)
	}
	return resultErr
}

func closeMetadataMigrationConn(conn *sql.Conn) error {
	err := conn.Close()
	if errors.Is(err, sql.ErrConnDone) {
		return nil
	}
	return wrapMigrationLockError(err, "close metadata migration lock connection")
}

func wrapMigrationLockError(err error, message string) error {
	if err == nil {
		return nil
	}
	return wrapError(err, "%s", message)
}

func discardMetadataMigrationConn(conn *sql.Conn) error {
	err := conn.Raw(func(any) error { return driver.ErrBadConn })
	if err == nil || errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) {
		return nil
	}
	return wrapError(err, "discard metadata migration lock connection")
}

func (mysqlMetadataMigrationLocker) SessionLock(ctx context.Context, conn *sql.Conn) error {
	name, err := mysqlMetadataMigrationLockName(ctx, conn)
	if err != nil {
		return err
	}
	var acquired sql.NullInt64
	err = conn.QueryRowContext(
		ctx,
		"SELECT GET_LOCK(?, ?)",
		name,
		metadataMigrationMySQLWaitSeconds,
	).Scan(&acquired)
	if err != nil {
		return wrapError(err, "acquire mysql metadata migration lock")
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return errorf("acquire mysql metadata migration lock: lock was not granted")
	}
	return nil
}

func (mysqlMetadataMigrationLocker) SessionUnlock(ctx context.Context, conn *sql.Conn) error {
	name, err := mysqlMetadataMigrationLockName(ctx, conn)
	if err != nil {
		return err
	}
	var released sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", name).Scan(&released); err != nil {
		return wrapError(err, "release mysql metadata migration lock")
	}
	if !released.Valid || released.Int64 != 1 {
		return errorf("release mysql metadata migration lock: lock was not released")
	}
	return nil
}

func mysqlMetadataMigrationLockName(ctx context.Context, conn *sql.Conn) (string, error) {
	var databaseName string
	if err := conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&databaseName); err != nil {
		return "", wrapError(err, "read mysql database name for migration lock")
	}
	if databaseName == "" {
		return "", errorf("%w: mysql metadata migration requires a selected database", ErrInvalidValue)
	}
	lockKey := sha256.Sum256([]byte("regimux.metadata.schema.migration:" + databaseName))
	return hex.EncodeToString(lockKey[:]), nil
}
