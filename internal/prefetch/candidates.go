// Package prefetch contains pure prefetch planning helpers.
package prefetch

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	collectionset "github.com/arcgolabs/collectionx/set"
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
	raw      string
	prefix   string
	segments int
	version  *semver.Version
	suffix   string
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
	seen := collectionset.NewOrderedSetWithCapacity[string](len(tags))
	parsed := make([]versionTag, 0, len(tags))
	for _, tag := range tags {
		if seen.Contains(tag) {
			continue
		}
		seen.Add(tag)

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

	clean, prefix := normalizeTagPrefix(raw)
	parts := strings.SplitN(clean, "-", 2)
	if strings.TrimSpace(parts[0]) == "" {
		return versionTag{}, false
	}

	base := parts[0]
	normalizedBase, segments, ok := normalizeSemverBase(base)
	if !ok {
		return versionTag{}, false
	}

	suffix := ""
	if len(parts) == 2 {
		suffix = strings.TrimSpace(parts[1])
		if suffix == "" {
			return versionTag{}, false
		}
	}

	versionText := normalizedBase
	if suffix != "" {
		versionText += "-" + normalizeSemverSuffix(suffix)
	}
	parsed, err := semver.NewVersion(versionText)
	if err != nil {
		return versionTag{}, false
	}

	return versionTag{
		raw:      raw,
		prefix:   prefix,
		segments: segments,
		version:  parsed,
		suffix:   suffix,
	}, true
}

func isCompatibleCandidate(source, target versionTag, maxDistance int) bool {
	if target.raw == source.raw {
		return false
	}
	if !strings.EqualFold(source.prefix, target.prefix) {
		return false
	}
	if source.suffix != target.suffix {
		return false
	}
	if source.segments > target.segments {
		return false
	}
	if compareVersionSegments(source.version, target.version, source.segments) >= 0 {
		return false
	}

	distance := versionDistance(source, target)
	return distance > 0 && distance <= maxDistance
}

func normalizeTagPrefix(raw string) (string, string) {
	prefix := ""
	if strings.HasPrefix(raw, "v") || strings.HasPrefix(raw, "V") {
		prefix = raw[:1]
		raw = raw[1:]
	}
	return raw, prefix
}

func normalizeSemverBase(raw string) (string, int, bool) {
	parts := strings.Split(raw, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return "", 0, false
	}

	for _, part := range parts {
		if part == "" {
			return "", 0, false
		}
	}
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return "", 0, false
	}
	segments := len(parts)
	for i := segments; i < 3; i++ {
		normalized += ".0"
	}
	return normalized, segments, true
}

func normalizeSemverSuffix(raw string) string {
	return strings.ReplaceAll(raw, "_", "-")
}

func compareVersionSegments(left, right *semver.Version, segments int) int {
	for i := 0; i < segments; i++ {
		leftSegment := getVersionSegment(left, i)
		rightSegment := getVersionSegment(right, i)
		if leftSegment < rightSegment {
			return -1
		}
		if leftSegment > rightSegment {
			return 1
		}
	}
	return 0
}

func getVersionSegment(version *semver.Version, index int) int {
	if version == nil {
		return 0
	}
	switch index {
	case 0:
		return int(version.Major())
	case 1:
		return int(version.Minor())
	case 2:
		return int(version.Patch())
	default:
		return 0
	}
}

func versionDistance(source, target versionTag) int {
	for i := 0; i < source.segments; i++ {
		sourceSegment := getVersionSegment(source.version, i)
		targetSegment := getVersionSegment(target.version, i)
		if sourceSegment != targetSegment {
			return targetSegment - sourceSegment
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

	distance := versionDistance(source, target)
	proximity := options.MaxVersionDistance - distance + 1
	if proximity < 0 {
		proximity = 0
	}

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
	for i := 0; i < source.segments; i++ {
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
	if count < 1 {
		return 1
	}
	return count
}

func compareTagNames(left, right string) int {
	leftVersion, leftOK := parseVersionTag(left)
	rightVersion, rightOK := parseVersionTag(right)
	if leftOK && rightOK {
		if versionCompare := compareVersionSegments(leftVersion.version, rightVersion.version, minVersionSegments(leftVersion, rightVersion)); versionCompare != 0 {
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

func minVersionSegments(left, right versionTag) int {
	if left.segments < right.segments {
		return left.segments
	}
	return right.segments
}
