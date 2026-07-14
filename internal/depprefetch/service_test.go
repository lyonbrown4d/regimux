package depprefetch_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/depprefetch"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestServicePrefetchesRecentScopedPulls(t *testing.T) {
	ctx := context.Background()
	store, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata store", err)
	t.Cleanup(func() { requireNoError(t, "close metadata store", store.Close()) })

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	recordPull(ctx, t, store, "npm/default", "left-pad", "metadata", now)
	recordPull(ctx, t, store, "hub", "library/alpine", "latest", now)

	var fetched []depprefetch.Candidate
	service := depprefetch.New(depprefetch.Dependencies{
		Ecosystem: ecosystem.NPM,
		Metadata:  store,
		Logger:    slog.New(slog.DiscardHandler),
		Fetch: func(_ context.Context, candidate depprefetch.Candidate) (depprefetch.FetchResult, error) {
			fetched = append(fetched, candidate)
			return depprefetch.FetchResult{BytesWarmed: 42}, nil
		},
	})
	report, err := service.Prefetch(ctx, ecosystem.PrefetchOptions{Now: now, MinPullCount: 1})
	requireNoError(t, "prefetch", err)

	assertPrefetchReport(t, report)
	assertFetchedCandidates(t, fetched)
	assertPrefetchHistory(ctx, t, store)
}

func TestServiceSkipsRecentlySuccessfulPrefetch(t *testing.T) {
	ctx := context.Background()
	store, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata store", err)
	t.Cleanup(func() { requireNoError(t, "close metadata store", store.Close()) })

	now := time.Now().UTC().Truncate(time.Second)
	recordPull(ctx, t, store, "npm/default", "left-pad", "metadata", now)

	var fetched []depprefetch.Candidate
	service := depprefetch.New(depprefetch.Dependencies{
		Ecosystem: ecosystem.NPM,
		Metadata:  store,
		Logger:    slog.New(slog.DiscardHandler),
		Fetch: func(_ context.Context, candidate depprefetch.Candidate) (depprefetch.FetchResult, error) {
			fetched = append(fetched, candidate)
			return depprefetch.FetchResult{BytesWarmed: 42}, nil
		},
	})

	opts := ecosystem.PrefetchOptions{Now: now, MinPullCount: 1, RetryWindow: time.Hour}
	first, err := service.Prefetch(ctx, opts)
	requireNoError(t, "first prefetch", err)
	if first.Prefetched != 1 || len(fetched) != 1 {
		t.Fatalf("unexpected first prefetch: report=%#v fetched=%#v", first, fetched)
	}

	opts.Now = now.Add(30 * time.Minute)
	second, err := service.Prefetch(ctx, opts)
	requireNoError(t, "second prefetch", err)
	if second.Prefetched != 0 || second.SkippedCandidates != 1 || len(fetched) != 1 {
		t.Fatalf("unexpected second prefetch: report=%#v fetched=%#v", second, fetched)
	}
}

func TestServiceDoesNotCountSuccessfulPrefetchWhenOutcomeRecordFails(t *testing.T) {
	ctx := context.Background()
	db, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata store", err)
	t.Cleanup(func() { requireNoError(t, "close metadata store", db.Close()) })

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	recordPull(ctx, t, db, "npm/default", "left-pad", "metadata", now)

	service := depprefetch.New(depprefetch.Dependencies{
		Ecosystem: ecosystem.NPM,
		Metadata:  &failingOutcomeMetaStore{SQLStore: db},
		Logger:    slog.New(slog.DiscardHandler),
		Fetch: func(_ context.Context, candidate depprefetch.Candidate) (depprefetch.FetchResult, error) {
			return depprefetch.FetchResult{BytesWarmed: 42}, nil
		},
	})

	report, err := service.Prefetch(ctx, ecosystem.PrefetchOptions{Now: now, MinPullCount: 1})
	if err == nil {
		t.Fatalf("expected prefetch to fail: %v", err)
	}
	if report == nil {
		t.Fatal("expected prefetch report")
		return
	}
	if report.Prefetched != 0 || report.BytesWarmed != 0 || report.Failed != 0 {
		t.Fatalf("unexpected report: %#v", report)
	}
	assertPrefetchHistoryWithoutOutcomes(ctx, t, db)
	assertPrefetchRunFailed(ctx, t, db)
}

func assertPrefetchReport(t *testing.T, report *ecosystem.PrefetchReport) {
	t.Helper()
	if report.Prefetched != 1 || report.Candidates != 1 || report.BytesWarmed != 42 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func assertFetchedCandidates(t *testing.T, fetched []depprefetch.Candidate) {
	t.Helper()
	if len(fetched) != 1 || fetched[0].Alias != "default" || fetched[0].Repository != "left-pad" {
		t.Fatalf("unexpected fetched candidates: %#v", fetched)
	}
}

func assertPrefetchHistory(ctx context.Context, t *testing.T, store meta.Store) {
	t.Helper()
	runs, err := store.ListPrefetchRuns(ctx, meta.PrefetchRunListRecentFirst(), meta.PrefetchRunListLimit(1))
	requireNoError(t, "list prefetch runs", err)
	if len(runs) != 1 || runs[0].Trigger != ecosystem.NPM || runs[0].Status != "completed" {
		t.Fatalf("unexpected prefetch runs: %#v", runs)
	}
	outcomes, err := store.ListPrefetchOutcomes(ctx, meta.PrefetchOutcomeListRecentFirst(), meta.PrefetchOutcomeListLimit(1))
	requireNoError(t, "list prefetch outcomes", err)
	if len(outcomes) != 1 || outcomes[0].Alias != "npm/default" || outcomes[0].Status != "success" {
		t.Fatalf("unexpected prefetch outcomes: %#v", outcomes)
	}
}

func assertPrefetchHistoryWithoutOutcomes(ctx context.Context, t *testing.T, store meta.Store) {
	t.Helper()
	outcomes, err := store.ListPrefetchOutcomes(ctx, meta.PrefetchOutcomeListRecentFirst(), meta.PrefetchOutcomeListLimit(1))
	requireNoError(t, "list prefetch outcomes", err)
	if len(outcomes) != 0 {
		t.Fatalf("unexpected prefetch outcomes: %#v", outcomes)
	}
}

func assertPrefetchRunFailed(ctx context.Context, t *testing.T, store meta.Store) {
	t.Helper()
	runs, err := store.ListPrefetchRuns(ctx, meta.PrefetchRunListRecentFirst(), meta.PrefetchRunListLimit(1))
	requireNoError(t, "list prefetch runs", err)
	if len(runs) != 1 || runs[0].Status != "failed" || runs[0].Prefetched != 0 || runs[0].BytesWarmed != 0 {
		t.Fatalf("unexpected prefetch run: %#v", runs)
	}
}

func recordPull(ctx context.Context, t *testing.T, store meta.Store, alias, repository, reference string, at time.Time) {
	t.Helper()
	_, err := store.RecordPull(ctx, meta.PullKey{Alias: alias, Repository: repository, Reference: reference}, at)
	requireNoError(t, "record pull", err)
}

func requireNoError(t *testing.T, action string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}

type failingOutcomeMetaStore struct {
	*meta.SQLStore
}

func (s *failingOutcomeMetaStore) CreatePrefetchOutcome(context.Context, meta.PrefetchOutcomeRecord) (*meta.PrefetchOutcomeRecord, error) {
	return nil, errors.New("forced prefetch outcome write failure")
}
