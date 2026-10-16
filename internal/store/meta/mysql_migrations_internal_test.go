package meta

import (
	"context"
	"os"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

func TestMySQLMigrationConfigPreservesDSNOptions(t *testing.T) {
	const dsn = "user:pass@tcp(localhost:3306)/regimux?parseTime=true&timeout=5s&multiStatements=false"
	config, err := mysqlMigrationConfig(dsn)
	if err != nil {
		t.Fatalf("configure migration DSN: %v", err)
	}
	if !config.MultiStatements || !config.ParseTime || config.Timeout != 5*time.Second {
		t.Fatalf("migration config lost existing DSN options: %#v", config)
	}
	original, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse original DSN: %v", err)
	}
	if original.MultiStatements {
		t.Fatal("migration config changed the caller's original DSN")
	}
}

func TestOpenMetadataDBMySQLMigrationUsesDedicatedConnection(t *testing.T) {
	dsn := os.Getenv("MYSQL_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("set MYSQL_MIGRATION_TEST_DSN to a fresh MySQL database")
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse migration test DSN: %v", err)
	}
	if config.MultiStatements {
		t.Fatal("MYSQL_MIGRATION_TEST_DSN must omit multiStatements=true")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for range 2 {
		db, openErr := OpenMetadataDB(ctx, DBOptions{Driver: metaDriverMySQL, DSN: dsn})
		if openErr != nil {
			t.Fatalf("open MySQL metadata DB: %v", openErr)
		}
		_, execErr := db.SQLDB().ExecContext(ctx, "SELECT 1; SELECT 2")
		closeErr := db.Close()
		if closeErr != nil {
			t.Fatalf("close MySQL metadata DB: %v", closeErr)
		}
		if execErr == nil {
			t.Fatal("business connection unexpectedly enabled multiStatements")
		}
	}
}
