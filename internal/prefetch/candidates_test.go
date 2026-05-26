// Package prefetch_test verifies prefetch candidate generation through exported APIs.
package prefetch_test

import (
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/prefetch"
)

func TestGenerateCandidatesMajorTags(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	got := prefetch.GenerateCandidates(
		[]prefetch.PullRecord{{
			Alias:      "hub",
			Repo:       "library/node",
			Tag:        "20",
			Count:      12,
			LastPullAt: now.Add(-30 * time.Minute),
		}},
		[]string{"latest", "18", "20", "20.1", "22", "22-alpine", "24", "25", "26", "30"},
		prefetch.Options{MaxCandidates: 3, MaxVersionDistance: 5, Now: now},
	)

	assertCandidateTags(t, got, []string{"22", "24", "25"})
	for _, candidate := range got {
		if candidate.Alias != "hub" || candidate.Repo != "library/node" {
			t.Fatalf("candidate route = %s/%s, want hub/library/node", candidate.Alias, candidate.Repo)
		}
		if candidate.SourceTag != "20" {
			t.Fatalf("source tag = %q, want 20", candidate.SourceTag)
		}
		if candidate.Score <= 0 || candidate.Reason == "" {
			t.Fatalf("candidate missing debug data: %+v", candidate)
		}
	}
}

func TestGenerateCandidatesPreservesSuffix(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	got := prefetch.GenerateCandidates(
		[]prefetch.PullRecord{{
			Alias:      "hub",
			Repo:       "library/node",
			Tag:        "20-alpine",
			Count:      5,
			LastPullAt: now.Add(-2 * time.Hour),
		}},
		[]string{"20", "21", "21-alpine", "22-bookworm", "22-alpine", "23-alpine3.19"},
		prefetch.Options{MaxCandidates: 5, MaxVersionDistance: 5, Now: now},
	)

	assertCandidateTags(t, got, []string{"21-alpine", "22-alpine"})
	for _, candidate := range got {
		if candidate.SourceTag != "20-alpine" {
			t.Fatalf("source tag = %q, want 20-alpine", candidate.SourceTag)
		}
	}
}

func TestGenerateCandidatesSemverLikeTags(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	got := prefetch.GenerateCandidates(
		[]prefetch.PullRecord{{
			Alias:      "ghcr",
			Repo:       "org/app",
			Tag:        "1.2",
			Count:      2,
			LastPullAt: now.Add(-12 * time.Hour),
		}},
		[]string{"1.2", "1.2.1", "1.3", "1.3-alpine", "1.4", "1.8", "2.0"},
		prefetch.Options{MaxCandidates: 3, MaxVersionDistance: 3, Now: now},
	)

	assertCandidateTags(t, got, []string{"1.3", "1.4", "2.0"})
}

func TestGenerateCandidatesSkipsLatestAsSource(t *testing.T) {
	t.Parallel()

	got := prefetch.GenerateCandidates(
		[]prefetch.PullRecord{{
			Alias: "hub",
			Repo:  "library/node",
			Tag:   "latest",
			Count: 100,
		}},
		[]string{"20", "21", "22"},
		prefetch.Options{MaxCandidates: 5},
	)

	if len(got) != 0 {
		t.Fatalf("candidates = %+v, want none", got)
	}
}

func TestGenerateCandidatesRequiresAvailableTagsAndAggregatesSignals(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	got := prefetch.GenerateCandidates(
		[]prefetch.PullRecord{
			{
				Alias:      "hub",
				Repo:       "library/node",
				Tag:        "20",
				Count:      1,
				LastPullAt: now.Add(-24 * time.Hour),
			},
			{
				Alias:      "hub",
				Repo:       "library/node",
				Tag:        "21",
				Count:      8,
				LastPullAt: now.Add(-time.Hour),
			},
		},
		[]string{"22", "99"},
		prefetch.Options{MaxCandidates: 5, MaxVersionDistance: 5, Now: now},
	)

	assertCandidateTags(t, got, []string{"22"})
	if got[0].SourceTag != "21" {
		t.Fatalf("source tag = %q, want strongest source 21", got[0].SourceTag)
	}
	if got[0].Score <= 200 {
		t.Fatalf("score = %d, want aggregated score above one weak signal", got[0].Score)
	}
}

func assertCandidateTags(t *testing.T, got []prefetch.Candidate, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("candidate count = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, tag := range want {
		if got[i].Tag != tag {
			t.Fatalf("candidate[%d].Tag = %q, want %q; all candidates: %+v", i, got[i].Tag, tag, got)
		}
	}
}
