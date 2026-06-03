package depprefetch_test

import (
	"context"
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
