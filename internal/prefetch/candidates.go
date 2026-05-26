// Package prefetch contains pure prefetch planning helpers.
package prefetch

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxCandidates      = 5
	defaultMaxVersionDistance = 5
)

var tagPattern = regexp.MustCompile(`^([vV]?)(\d+(?:\.\d+){0,2})(?:-([A-Za-z0-9_][A-Za-z0-9_.-]*))?$`)

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

	candidates := map[candidateKey]candidateAccumulator{}
	for _, record := range records {
		source, ok := parseVersionTag(record.Tag)
		if !ok {
			continue
		}
		for _, target := range available {
			if !isCompatibleCandidate(source, target, options.MaxVersionDistance) {
				continue
			}

			score := scoreCandidate(record, source, target, options)
			key := candidateKey{alias: record.Alias, repo: record.Repo, tag: target.raw}
			reason := candidateReason(record, source, target)
			accumulator := candidates[key]
			accumulator.score += score
			if score > accumulator.bestScore {
				accumulator.bestScore = score
				accumulator.candidate = Candidate{
					Alias:     record.Alias,
					Repo:      record.Repo,
					Tag:       target.raw,
					SourceTag: record.Tag,
					Reason:    reason,
				}
			}
			candidates[key] = accumulator
		}
	}

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

	if len(results) > options.MaxCandidates {
		results = results[:options.MaxCandidates]
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

type versionTag struct {
	raw     string
	prefix  string
	numbers []int
	suffix  string
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

func parseAvailableTags(tags []string) []versionTag {
	seen := map[string]struct{}{}
	parsed := make([]versionTag, 0, len(tags))
	for _, tag := range tags {
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}

		version, ok := parseVersionTag(tag)
		if !ok {
			continue
		}
		parsed = append(parsed, version)
	}
	return parsed
}

func parseVersionTag(tag string) (versionTag, bool) {
	raw := strings.TrimSpace(tag)
	if raw == "" || strings.EqualFold(raw, "latest") {
		return versionTag{}, false
	}

	matches := tagPattern.FindStringSubmatch(raw)
	if matches == nil {
		return versionTag{}, false
	}

	parts := strings.Split(matches[2], ".")
	numbers := make([]int, 0, len(parts))
	for _, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil {
			return versionTag{}, false
		}
		numbers = append(numbers, number)
	}

	return versionTag{
		raw:     raw,
		prefix:  matches[1],
		numbers: numbers,
		suffix:  matches[3],
	}, true
}

func isCompatibleCandidate(source, target versionTag, maxDistance int) bool {
	if target.raw == source.raw {
		return false
	}
	if source.prefix != target.prefix {
		return false
	}
	if source.suffix != target.suffix {
		return false
	}
	if len(source.numbers) != len(target.numbers) {
		return false
	}
	if compareNumbers(source.numbers, target.numbers) >= 0 {
		return false
	}

	distance := versionDistance(source.numbers, target.numbers)
	return distance > 0 && distance <= maxDistance
}

func compareNumbers(left, right []int) int {
	length := len(left)
	if len(right) < length {
		length = len(right)
	}
	for i := range length {
		if left[i] < right[i] {
			return -1
		}
		if left[i] > right[i] {
			return 1
		}
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return 0
}

func versionDistance(source, target []int) int {
	for i := range source {
		if source[i] != target[i] {
			return target[i] - source[i]
		}
	}
	return 0
}

func scoreCandidate(record PullRecord, source, target versionTag, options Options) int {
	count := record.Count
	if count < 1 {
		count = 1
	}
	if count > 1000 {
		count = 1000
	}

	distance := versionDistance(source.numbers, target.numbers)
	proximity := options.MaxVersionDistance - distance + 1
	if proximity < 0 {
		proximity = 0
	}

	score := 100
	score += count * 10
	score += proximity * 20
	score += recencyScore(record.LastPullAt, options.Now)
	if len(source.numbers) > 1 && source.numbers[0] == target.numbers[0] {
		score += 25
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
	change := changedVersionPart(source.numbers, target.numbers)
	suffix := "without suffix"
	if source.suffix != "" {
		suffix = fmt.Sprintf("with suffix %q", source.suffix)
	}
	return fmt.Sprintf("observed %s pulled %d times; %s is an available newer %s tag %s", record.Tag, normalizedCount(record.Count), target.raw, change, suffix)
}

func changedVersionPart(source, target []int) string {
	names := []string{"major", "minor", "patch"}
	for i := range source {
		if source[i] != target[i] {
			if i < len(names) {
				return names[i]
			}
			return "version"
		}
	}
	return "version"
}

func normalizedCount(count int) int {
	if count < 1 {
		return 1
	}
	return count
}

func compareTagNames(left, right string) int {
	leftVersion, leftOK := parseVersionTag(left)
	rightVersion, rightOK := parseVersionTag(right)
	if leftOK && rightOK {
		if versionCompare := compareNumbers(leftVersion.numbers, rightVersion.numbers); versionCompare != 0 {
			return versionCompare
		}
		if leftVersion.suffix < rightVersion.suffix {
			return -1
		}
		if leftVersion.suffix > rightVersion.suffix {
			return 1
		}
	}
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
