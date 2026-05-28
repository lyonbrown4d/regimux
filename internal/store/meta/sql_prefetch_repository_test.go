package meta_test

import (
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestSQLStorePrefetchPolicyRecords(t *testing.T) {
	ctx := context.Background()
	store := newSQLStore(ctx, t)
	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)

	run, err := store.CreatePrefetchRun(ctx, meta.PrefetchRunRecord{
		Status:     "running",
		StartedAt:  now,
		ByteBudget: 1024,
		TaskBudget: 2,
	})
	requireNoError(t, "create prefetch run", err)
	run.Status = "completed"
	run.FinishedAt = now.Add(time.Minute)
	run.Candidates = 1
	run.Prefetched = 1
	run.BytesWarmed = 128
	_, err = store.UpdatePrefetchRun(ctx, *run)
	requireNoError(t, "update prefetch run", err)

	outcome, err := store.CreatePrefetchOutcome(ctx, meta.PrefetchOutcomeRecord{
		RunID:           run.ID,
		Alias:           "hub",
		Repository:      "library/node",
		Reference:       "25",
		SourceReference: "20",
		Status:          "success",
		Attempt:         1,
		BytesWarmed:     128,
		FinishedAt:      now.Add(time.Minute),
	})
	requireNoError(t, "create prefetch outcome", err)
	if outcome.CandidateKey != "hub/library/node:25" {
		t.Fatalf("unexpected prefetch outcome key: %#v", outcome)
	}
	latest, ok, err := store.LatestPrefetchOutcome(ctx, meta.PrefetchCandidateKey{Alias: "hub", Repository: "library/node", Reference: "25"})
	requireNoError(t, "latest prefetch outcome", err)
	if !ok || latest.ID != outcome.ID {
		t.Fatalf("unexpected latest prefetch outcome: ok=%v record=%#v", ok, latest)
	}

	control, err := store.RequestPrefetchControl(ctx, meta.PrefetchControlRecord{Action: "retry", RequestedAt: now})
	requireNoError(t, "request prefetch control", err)
	consumed, ok, err := store.ConsumePrefetchControl(ctx, "retry", now.Add(time.Second))
	requireNoError(t, "consume prefetch control", err)
	if !ok || consumed.ID != control.ID || consumed.ConsumedAt.IsZero() {
		t.Fatalf("unexpected consumed control: ok=%v record=%#v", ok, consumed)
	}
}
