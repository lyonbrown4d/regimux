// Package prefetch contains pure prefetch planning helpers.
package prefetch

import (
	"fmt"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
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
func GenerateCandidates(
	records *collectionlist.List[PullRecord],
	availableTags *collectionlist.List[string],
	options Options,
) *collectionlist.List[Candidate] {
	if records == nil || records.IsEmpty() || availableTags == nil || availableTags.IsEmpty() {
		return collectionlist.NewList[Candidate]()
	}

	options = normalizeOptions(options)
	available := parseAvailableTags(availableTags)
	if available.IsEmpty() {
		return collectionlist.NewList[Candidate]()
	}

	candidates := accumulateCandidates(records, available, options)
	return sortedCandidates(candidates, options.MaxCandidates)
}

func accumulateCandidates(
	records *collectionlist.List[PullRecord],
	available *collectionlist.List[versionTag],
	options Options,
) *collectionmapping.Map[candidateKey, candidateAccumulator] {
	candidates := collectionmapping.NewMap[candidateKey, candidateAccumulator]()
	records.Range(func(_ int, record PullRecord) bool {
		addRecordCandidates(candidates, record, available, options)
		return true
	})
	return candidates
}

func addRecordCandidates(
	candidates *collectionmapping.Map[candidateKey, candidateAccumulator],
	record PullRecord,
	available *collectionlist.List[versionTag],
	options Options,
) {
	source, ok := parseVersionTag(record.Tag)
	if !ok {
		return
	}
	available.Range(func(_ int, target versionTag) bool {
		if !isCompatibleCandidate(source, target, options.MaxVersionDistance) {
			return true
		}
		addCandidate(candidates, record, source, target, scoreCandidate(record, source, target, options))
		return true
	})
}

func addCandidate(
	candidates *collectionmapping.Map[candidateKey, candidateAccumulator],
	record PullRecord,
	source versionTag,
	target versionTag,
	score int,
) {
	key := candidateKey{alias: record.Alias, repo: record.Repo, tag: target.raw}
	accumulator, _ := candidates.Get(key)
	accumulator.score += score
	if score > accumulator.bestScore {
		accumulator.bestScore = score
		accumulator.candidate = newCandidate(record, source, target)
	}
	candidates.Set(key, accumulator)
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

func sortedCandidates(
	candidates *collectionmapping.Map[candidateKey, candidateAccumulator],
	limit int,
) *collectionlist.List[Candidate] {
	if candidates == nil || candidates.IsEmpty() {
		return collectionlist.NewList[Candidate]()
	}

	results := collectionlist.MapList(collectionlist.NewList(candidates.Values()...), func(_ int, accumulator candidateAccumulator) Candidate {
		candidate := accumulator.candidate
		candidate.Score = accumulator.score
		return candidate
	})
	results.Sort(compareCandidatePriority)

	if results.Len() > limit {
		return results.Take(limit)
	}
	return results
}

func compareCandidatePriority(left, right Candidate) int {
	if score := compareIntDesc(left.Score, right.Score); score != 0 {
		return score
	}
	if alias := compareStringAsc(left.Alias, right.Alias); alias != 0 {
		return alias
	}
	if repo := compareStringAsc(left.Repo, right.Repo); repo != 0 {
		return repo
	}
	return compareTagNames(left.Tag, right.Tag)
}

func compareIntDesc(left, right int) int {
	switch {
	case left > right:
		return -1
	case left < right:
		return 1
	default:
		return 0
	}
}

func compareStringAsc(left, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
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
