package suggestion

import (
	"strings"
	"unicode"

	"github.com/agext/levenshtein"
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/lo"
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
	if left.score > right.score {
		return -1
	}
	if left.score < right.score {
		return 1
	}
	if left.distance < right.distance {
		return -1
	}
	if left.distance > right.distance {
		return 1
	}
	if left.value < right.value {
		return -1
	}
	if left.value > right.value {
		return 1
	}
	return 0
}
