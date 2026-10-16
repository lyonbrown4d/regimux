package meta_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestConcurrentMetadataDBInitialization(t *testing.T) {
	driver := strings.ToLower(os.Getenv("REGIMUX_META_CONCURRENCY_DRIVER"))
	dsn := os.Getenv("REGIMUX_META_CONCURRENCY_DSN")
	if (driver != "postgres" && driver != "mysql") || dsn == "" {
		t.Skip("set REGIMUX_META_CONCURRENCY_DRIVER and REGIMUX_META_CONCURRENCY_DSN to a fresh database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	const openCount = 8
	start := make(chan struct{})
	results := make(chan metadataOpenResult, openCount)
	for index := range openCount {
		go openMetadataDBAfterStart(ctx, start, results, index, driver, dsn)
	}
	close(start)
	opened, failures := collectMetadataOpenResults(results, openCount)
	closeMetadataDBs(t, opened)
	for _, failure := range failures {
		t.Error(failure)
	}
	reopened, err := meta.OpenMetadataDB(ctx, meta.DBOptions{Driver: driver, DSN: dsn})
	if err != nil {
		t.Fatalf("reopen metadata DB after concurrent initialization: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened metadata DB: %v", err)
	}
}

type metadataOpenResult struct {
	index int
	db    *dbx.DB
	err   error
}

func openMetadataDBAfterStart(
	ctx context.Context,
	start <-chan struct{},
	results chan<- metadataOpenResult,
	index int,
	driver, dsn string,
) {
	<-start
	db, err := meta.OpenMetadataDB(ctx, meta.DBOptions{Driver: driver, DSN: dsn})
	results <- metadataOpenResult{index: index, db: db, err: err}
}

func collectMetadataOpenResults(results <-chan metadataOpenResult, count int) ([]*dbx.DB, []error) {
	opened := make([]*dbx.DB, 0, count)
	failures := make([]error, 0)
	for range count {
		result := <-results
		if result.err != nil {
			failures = append(failures, fmt.Errorf("concurrent metadata DB open %d: %w", result.index, result.err))
			continue
		}
		opened = append(opened, result.db)
	}
	return opened, failures
}

func closeMetadataDBs(t *testing.T, databases []*dbx.DB) {
	t.Helper()
	for index, db := range databases {
		if err := db.Close(); err != nil {
			t.Errorf("close concurrently opened metadata DB %d: %v", index, err)
		}
	}
}
