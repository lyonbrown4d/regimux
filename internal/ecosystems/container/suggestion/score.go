package suggestion

import (
	"cmp"
	"strings"
	"unicode"

	"github.com/agext/levenshtein"
	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionprefix "github.com/arcgolabs/collectionx/prefix"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/lo"
)

const (
	minPrefixCandidateLength = 3
	minPrefixSearchValues    = 64
)

type SuggestOptions struct {
	Limit int
}

func SuggestTags(reference string, tags []string, opts SuggestOptions) []string {
	if opts.Limit <= 0 {
		opts.Limit = defaultMaxSuggestions
	}
	return rankTags(reference, tags, opts.Limit)
}

func SuggestRepositories(repository string, repositories []string, opts SuggestOptions) []string {
	if opts.Limit <= 0 {
		opts.Limit = defaultMaxSuggestions
	}
	return rankRepositories(repository, repositories, opts.Limit)
}

func manifestSuggestions(alias, repo string, tags []string) []distribution.ManifestSuggestion {
	return lo.Map(tags, func(tag string, _ int) distribution.ManifestSuggestion {
		return distribution.ManifestSuggestion{
			Reference: tag,
			Image:     suggestedImage(alias, repo, tag),
		}
	})
}

func suggestedImage(alias, repo, tag string) string {
	name := strings.Trim(strings.Trim(alias, "/")+"/"+strings.Trim(repo, "/"), "/")
	if name == "" {
		return tag
	}
	return name + ":" + tag
}

type scoredValue struct {
	value    string
	score    int
	distance int
}

func rankTags(reference string, tags []string, limit int) []string {
	return rankValues(reference, tags, limit)
}

func rankRepositories(repository string, repositories []string, limit int) []string {
	return rankValues(repository, repositories, limit)
}

func rankValues(target string, values []string, limit int) []string {
	if candidates, ok := prefixCandidateValues(target, values, limit); ok {
		ranked := rankCandidateValues(target, candidates, limit)
		if len(ranked) >= limit {
			return ranked
		}
	}
	return rankCandidateValues(target, values, limit)
}

func rankCandidateValues(target string, values []string, limit int) []string {
	scored := collectionlist.NewList(lo.FilterMap(values, func(value string, _ int) (scoredValue, bool) {
		value = strings.TrimSpace(value)
		if value == "" || value == target {
			return scoredValue{}, false
		}
		score := suggestionScore(target, value)
		if score <= 0 {
			return scoredValue{}, false
		}
		return scoredValue{value: value, score: score, distance: levenshtein.Distance(target, value, nil)}, true
	})...)
	if scored.IsEmpty() {
		return nil
	}
	scored.Sort(compareScoredValue)
	if scored.Len() > limit {
		scored = scored.Take(limit)
	}
	return lo.Map(scored.Values(), func(item scoredValue, _ int) string {
		return item.value
	})
}

func prefixCandidateValues(target string, values []string, limit int) ([]string, bool) {
	target, ok := prefixSearchTarget(target, values, limit)
	if !ok {
		return nil, false
	}

	trie := buildPrefixTrie(target, values)
	if trie.IsEmpty() {
		return nil, false
	}
	return trieCandidatesForTargetPrefix(trie, target, limit)
}

func prefixSearchTarget(target string, values []string, limit int) (string, bool) {
	target = strings.ToLower(strings.TrimSpace(target))
	if len(target) < minPrefixCandidateLength || len(values) < minPrefixSearchValues || limit <= 0 {
		return "", false
	}
	return target, true
}

func buildPrefixTrie(target string, values []string) *collectionprefix.Trie[string] {
	trie := collectionprefix.NewTrie[string]()
	for _, value := range values {
		addPrefixCandidate(trie, target, value)
	}
	return trie
}

func addPrefixCandidate(trie *collectionprefix.Trie[string], target, value string) {
	value = strings.TrimSpace(value)
	key := strings.ToLower(value)
	if key == "" || key == target || trie.Has(key) {
		return
	}
	trie.Put(key, value)
}

func trieCandidatesForTargetPrefix(
	trie *collectionprefix.Trie[string],
	target string,
	limit int,
) ([]string, bool) {
	for prefixLength := len(target); prefixLength >= minPrefixCandidateLength; prefixLength-- {
		prefix := target[:prefixLength]
		count := trie.CountPrefix(prefix)
		if count == 0 {
			continue
		}
		if count < limit && prefixLength > minPrefixCandidateLength {
			continue
		}
		return trie.ValuesWithPrefix(prefix), count >= limit
	}
	return nil, false
}

func suggestionScore(target, value string) int {
	target = strings.ToLower(strings.TrimSpace(target))
	value = strings.ToLower(strings.TrimSpace(value))
	if target == "" || value == "" {
		return 0
	}

	score := sharedDigitBonus(target, value)
	score += commonPrefixLength(target, value) * 3
	score += commonSuffixLength(target, value) * 2
	score += substringBonus(target, value)
	score += editDistanceBonus(target, value)
	if score < 35 {
		return 0
	}
	return score
}

func substringBonus(target, value string) int {
	if strings.Contains(value, target) || strings.Contains(target, value) {
		return 80
	}
	return 0
}

func editDistanceBonus(target, value string) int {
	return max(40-levenshtein.Distance(target, value, nil)*4, 0)
}

func sharedDigitBonus(target, value string) int {
	score := 0
	for _, token := range digitTokens(target) {
		if strings.Contains(value, token) {
			score += 50
		}
	}
	return score
}

func digitTokens(value string) []string {
	var tokens []string
	start := -1
	for index, char := range value {
		if unicode.IsDigit(char) {
			if start < 0 {
				start = index
			}
			continue
		}
		if start >= 0 {
			tokens = append(tokens, value[start:index])
			start = -1
		}
	}
	if start >= 0 {
		tokens = append(tokens, value[start:])
	}
	return tokens
}

func commonPrefixLength(left, right string) int {
	limit := min(len(left), len(right))
	for i := range limit {
		if left[i] != right[i] {
			return i
		}
	}
	return limit
}

func commonSuffixLength(left, right string) int {
	limit := min(len(left), len(right))
	for i := range limit {
		if left[len(left)-1-i] != right[len(right)-1-i] {
			return i
		}
	}
	return limit
}

func compareScoredValue(left, right scoredValue) int {
	if score := cmp.Compare(right.score, left.score); score != 0 {
		return score
	}
	if distance := cmp.Compare(left.distance, right.distance); distance != 0 {
		return distance
	}
	return cmp.Compare(left.value, right.value)
}
