// Package prefetch_test verifies prefetch candidate generation through exported APIs.
package prefetch_test

import (
	"testing"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/prefetch"
)

func TestGenerateCandidatesMajorTags(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	got := prefetch.GenerateCandidates(
		collectionlist.NewList(prefetch.PullRecord{
			Alias:      "hub",
			Repo:       "library/node",
			Tag:        "20",
			Count:      12,
			LastPullAt: now.Add(-30 * time.Minute),
		}),
		collectionlist.NewList("latest", "18", "20", "20.1", "22", "22-alpine", "24", "25", "26", "30"),
		prefetch.Options{MaxCandidates: 3, MaxVersionDistance: 5, Now: now},
	)

	assertCandidateTags(t, got, []string{"22", "24", "25"})
	got.Range(func(_ int, candidate prefetch.Candidate) bool {
		if candidate.Alias != "hub" || candidate.Repo != "library/node" {
			t.Fatalf("candidate route = %s/%s, want hub/library/node", candidate.Alias, candidate.Repo)
		}
		if candidate.SourceTag != "20" {
			t.Fatalf("source tag = %q, want 20", candidate.SourceTag)
		}
		if candidate.Score <= 0 || candidate.Reason == "" {
			t.Fatalf("candidate missing debug data: %+v", candidate)
		}
		return true
	})
}

func TestGenerateCandidatesPreservesSuffix(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	got := prefetch.GenerateCandidates(
		collectionlist.NewList(prefetch.PullRecord{
			Alias:      "hub",
			Repo:       "library/node",
			Tag:        "20-alpine",
			Count:      5,
			LastPullAt: now.Add(-2 * time.Hour),
		}),
		collectionlist.NewList("20", "21", "21-alpine", "22-bookworm", "22-alpine", "23-alpine3.19"),
		prefetch.Options{MaxCandidates: 5, MaxVersionDistance: 5, Now: now},
	)

	assertCandidateTags(t, got, []string{"21-alpine", "22-alpine"})
	got.Range(func(_ int, candidate prefetch.Candidate) bool {
		if candidate.SourceTag != "20-alpine" {
			t.Fatalf("source tag = %q, want 20-alpine", candidate.SourceTag)
		}
		return true
	})
}

func TestGenerateCandidatesSemverLikeTags(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	got := prefetch.GenerateCandidates(
		collectionlist.NewList(prefetch.PullRecord{
			Alias:      "ghcr",
			Repo:       "org/app",
			Tag:        "1.2",
			Count:      2,
			LastPullAt: now.Add(-12 * time.Hour),
		}),
		collectionlist.NewList("1.2", "1.2.1", "1.3", "1.3-alpine", "1.4", "1.8", "2.0"),
		prefetch.Options{MaxCandidates: 3, MaxVersionDistance: 3, Now: now},
	)

	assertCandidateTags(t, got, []string{"1.3", "1.4", "2.0"})
}

func TestGenerateCandidatesSkipsLatestAsSource(t *testing.T) {
	t.Parallel()

	got := prefetch.GenerateCandidates(
		collectionlist.NewList(prefetch.PullRecord{
			Alias: "hub",
			Repo:  "library/node",
			Tag:   "latest",
			Count: 100,
		}),
		collectionlist.NewList("20", "21", "22"),
		prefetch.Options{MaxCandidates: 5},
	)

	if got.Len() != 0 {
		t.Fatalf("candidates = %+v, want none", got)
	}
}

func TestGenerateCandidatesRequiresAvailableTagsAndAggregatesSignals(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	got := prefetch.GenerateCandidates(
		collectionlist.NewList(
			prefetch.PullRecord{
				Alias:      "hub",
				Repo:       "library/node",
				Tag:        "20",
				Count:      1,
				LastPullAt: now.Add(-24 * time.Hour),
			},
			prefetch.PullRecord{
				Alias:      "hub",
				Repo:       "library/node",
				Tag:        "21",
				Count:      8,
				LastPullAt: now.Add(-time.Hour),
			},
		),
		collectionlist.NewList("22", "99"),
		prefetch.Options{MaxCandidates: 5, MaxVersionDistance: 5, Now: now},
	)

	assertCandidateTags(t, got, []string{"22"})
	candidate, ok := got.GetFirst()
	if !ok {
		t.Fatal("missing first candidate")
	}
	if candidate.SourceTag != "21" {
		t.Fatalf("source tag = %q, want strongest source 21", candidate.SourceTag)
	}
	if candidate.Score <= 200 {
		t.Fatalf("score = %d, want aggregated score above one weak signal", candidate.Score)
	}
}

func assertCandidateTags(t *testing.T, got *collectionlist.List[prefetch.Candidate], want []string) {
	t.Helper()

	if got.Len() != len(want) {
		t.Fatalf("candidate count = %d, want %d: %+v", got.Len(), len(want), got)
	}
	for i, tag := range want {
		candidate, ok := got.Get(i)
		if !ok || candidate.Tag != tag {
			t.Fatalf("candidate[%d].Tag = %q, want %q; all candidates: %+v", i, candidate.Tag, tag, got)
		}
	}
}
