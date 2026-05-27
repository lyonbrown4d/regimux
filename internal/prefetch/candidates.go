// Package prefetch contains pure prefetch planning helpers.
package prefetch

import (
	"fmt"
	"sort"
	"time"
)

const (
	defaultMaxCandidates      = 5
	defaultMaxVersionDistance = 5
)

// PullRecord is one observed pull signal for a repository tag.
type PullRecord struct {
	Alias      string
	Repo       string
	Tag        string
	Count      int
	LastPullAt time.Time
}

// Candidate is a tag worth considering for a future manifest prefetch.
type Candidate struct {
	Alias     string
	Repo      string
	Tag       string
	SourceTag string
	Reason    string
	Score     int
}

// Options tunes candidate generation.
type Options struct {
	// MaxCandidates limits the returned list. Values <= 0 use a conservative default.
	MaxCandidates int
	// MaxVersionDistance limits how far from the observed version a candidate may be.
	// Values <= 0 use a conservative default.
	MaxVersionDistance int
	// Now makes recency scoring deterministic for tests. Zero means time.Now().
	Now time.Time
}

// GenerateCandidates returns conservative tag candidates that already exist in availableTags.
func GenerateCandidates(records []PullRecord, availableTags []string, options Options) []Candidate {
	if len(records) == 0 || len(availableTags) == 0 {
		return nil
	}

	options = normalizeOptions(options)
	available := parseAvailableTags(availableTags)
	if len(available) == 0 {
		return nil
	}

	candidates := accumulateCandidates(records, available, options)
	return sortedCandidates(candidates, options.MaxCandidates)
}

func accumulateCandidates(records []PullRecord, available []versionTag, options Options) map[candidateKey]candidateAccumulator {
	candidates := make(map[candidateKey]candidateAccumulator)
	for _, record := range records {
		addRecordCandidates(candidates, record, available, options)
	}
	return candidates
}

func addRecordCandidates(candidates map[candidateKey]candidateAccumulator, record PullRecord, available []versionTag, options Options) {
	source, ok := parseVersionTag(record.Tag)
	if !ok {
		return
	}
	for _, target := range available {
		if !isCompatibleCandidate(source, target, options.MaxVersionDistance) {
			continue
		}
		addCandidate(candidates, record, source, target, scoreCandidate(record, source, target, options))
	}
}

func addCandidate(candidates map[candidateKey]candidateAccumulator, record PullRecord, source, target versionTag, score int) {
	key := candidateKey{alias: record.Alias, repo: record.Repo, tag: target.raw}
	accumulator := candidates[key]
	accumulator.score += score
	if score > accumulator.bestScore {
		accumulator.bestScore = score
		accumulator.candidate = newCandidate(record, source, target)
	}
	candidates[key] = accumulator
}

func newCandidate(record PullRecord, source, target versionTag) Candidate {
	return Candidate{
		Alias:     record.Alias,
		Repo:      record.Repo,
		Tag:       target.raw,
		SourceTag: record.Tag,
		Reason:    candidateReason(record, source, target),
	}
}

func sortedCandidates(candidates map[candidateKey]candidateAccumulator, limit int) []Candidate {
	if len(candidates) == 0 {
		return nil
	}

	results := make([]Candidate, 0, len(candidates))
	for _, accumulator := range candidates {
		candidate := accumulator.candidate
		candidate.Score = accumulator.score
		results = append(results, candidate)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Alias != results[j].Alias {
			return results[i].Alias < results[j].Alias
		}
		if results[i].Repo != results[j].Repo {
			return results[i].Repo < results[j].Repo
		}
		return compareTagNames(results[i].Tag, results[j].Tag) < 0
	})

	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

type candidateKey struct {
	alias string
	repo  string
	tag   string
}

type candidateAccumulator struct {
	candidate Candidate
	score     int
	bestScore int
}

func normalizeOptions(options Options) Options {
	if options.MaxCandidates <= 0 {
		options.MaxCandidates = defaultMaxCandidates
	}
	if options.MaxVersionDistance <= 0 {
		options.MaxVersionDistance = defaultMaxVersionDistance
	}
	if options.Now.IsZero() {
		options.Now = time.Now()
	}
	return options
}

func scoreCandidate(record PullRecord, source, target versionTag, options Options) int {
	count := min(max(record.Count, 1), 1000)

	distance := versionDistance(source, target)
	proximity := max(options.MaxVersionDistance-distance+1, 0)

	score := 100
	score += count * 10
	score += proximity * 20
	score += recencyScore(record.LastPullAt, options.Now)
	if source.segments > 1 {
		score += 10
		if getVersionSegment(source.version, 0) != getVersionSegment(target.version, 0) {
			score -= 40
		}
	}
	if source.suffix != "" {
		score += 25
	} else {
		score += 10
	}
	return score
}

func recencyScore(lastPullAt, now time.Time) int {
	if lastPullAt.IsZero() || now.IsZero() {
		return 0
	}
	if lastPullAt.After(now) {
		return 50
	}

	age := now.Sub(lastPullAt)
	switch {
	case age <= time.Hour:
		return 50
	case age <= 24*time.Hour:
		return 30
	case age <= 7*24*time.Hour:
		return 15
	case age <= 30*24*time.Hour:
		return 5
	default:
		return 0
	}
}

func candidateReason(record PullRecord, source, target versionTag) string {
	change := changedVersionPart(source, target)
	suffix := "without suffix"
	if source.suffix != "" {
		suffix = fmt.Sprintf("with suffix %q", source.suffix)
	}
	return fmt.Sprintf("observed %s pulled %d times; %s is an available newer %s tag %s", record.Tag, normalizedCount(record.Count), target.raw, change, suffix)
}

func changedVersionPart(source, target versionTag) string {
	names := []string{"major", "minor", "patch"}
	if source.version == nil || target.version == nil {
		return "version"
	}
	for i := range source.segments {
		if getVersionSegment(source.version, i) == getVersionSegment(target.version, i) {
			continue
		}
		if i < len(names) {
			return names[i]
		}
		return "version"
	}
	return "version"
}

func normalizedCount(count int) int {
	return max(count, 1)
}
