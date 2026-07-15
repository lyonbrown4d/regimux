package maven

import "strings"

type mavenVersionToken struct {
	value   string
	numeric bool
}

func compareMavenVersions(left, right string) int {
	leftTokens := normalizeMavenVersionTokens(tokenizeMavenVersion(left))
	rightTokens := normalizeMavenVersionTokens(tokenizeMavenVersion(right))
	limit := max(len(leftTokens), len(rightTokens))
	for index := range limit {
		comparison := compareMavenVersionTokenAt(leftTokens, rightTokens, index)
		if comparison != 0 {
			return comparison
		}
	}
	return 0
}

func tokenizeMavenVersion(version string) []mavenVersionToken {
	version = strings.ToLower(strings.TrimSpace(version))
	tokens := make([]mavenVersionToken, 0, 8)
	start := 0

	for index := range len(version) {
		if isMavenVersionSeparator(version[index]) {
			tokens = appendMavenVersionToken(tokens, version[start:index])
			start = index + 1
			continue
		}
		if index > start && isMavenDigit(version[index]) != isMavenDigit(version[index-1]) {
			tokens = appendMavenVersionToken(tokens, version[start:index])
			start = index
		}
	}
	return appendMavenVersionToken(tokens, version[start:])
}

func isMavenVersionSeparator(character byte) bool {
	return character == '.' || character == '-' || character == '_'
}

func isMavenDigit(character byte) bool {
	return character >= '0' && character <= '9'
}

func appendMavenVersionToken(
	tokens []mavenVersionToken,
	value string,
) []mavenVersionToken {
	if value == "" {
		return tokens
	}
	return append(tokens, mavenVersionToken{
		value:   value,
		numeric: isMavenDigit(value[0]),
	})
}

func normalizeMavenVersionTokens(tokens []mavenVersionToken) []mavenVersionToken {
	for len(tokens) > 0 {
		last := tokens[len(tokens)-1]
		if last.numeric && isZeroMavenNumber(last.value) {
			tokens = tokens[:len(tokens)-1]
			continue
		}
		if !last.numeric && canonicalMavenQualifier(last.value) == "" {
			tokens = tokens[:len(tokens)-1]
			continue
		}
		break
	}
	return tokens
}

func compareMavenVersionTokenAt(
	left []mavenVersionToken,
	right []mavenVersionToken,
	index int,
) int {
	if index >= len(left) && index >= len(right) {
		return 0
	}
	if index >= len(left) {
		return compareMissingMavenToken(right[index])
	}
	if index >= len(right) {
		return -compareMissingMavenToken(left[index])
	}
	return compareMavenVersionTokens(left[index], right[index])
}

func compareMissingMavenToken(token mavenVersionToken) int {
	if token.numeric {
		if isZeroMavenNumber(token.value) {
			return 0
		}
		return -1
	}
	return compareMavenQualifiers("", token.value)
}

func compareMavenVersionTokens(left, right mavenVersionToken) int {
	if left.numeric && right.numeric {
		return compareMavenNumbers(left.value, right.value)
	}
	if left.numeric {
		return 1
	}
	if right.numeric {
		return -1
	}
	return compareMavenQualifiers(left.value, right.value)
}

func compareMavenNumbers(left, right string) int {
	left = strings.TrimLeft(left, "0")
	right = strings.TrimLeft(right, "0")
	if len(left) != len(right) {
		if len(left) < len(right) {
			return -1
		}
		return 1
	}
	return strings.Compare(left, right)
}

func compareMavenQualifiers(left, right string) int {
	left = canonicalMavenQualifier(left)
	right = canonicalMavenQualifier(right)
	leftRank := mavenQualifierRank(left)
	rightRank := mavenQualifierRank(right)
	if leftRank != rightRank {
		if leftRank < rightRank {
			return -1
		}
		return 1
	}
	return strings.Compare(left, right)
}

func canonicalMavenQualifier(qualifier string) string {
	switch qualifier {
	case "a":
		return "alpha"
	case "b":
		return "beta"
	case "m":
		return "milestone"
	case "cr":
		return "rc"
	case "ga", "final", "release":
		return ""
	default:
		return qualifier
	}
}

func mavenQualifierRank(qualifier string) int {
	switch qualifier {
	case "alpha":
		return 0
	case "beta":
		return 1
	case "milestone":
		return 2
	case "rc":
		return 3
	case "snapshot":
		return 4
	case "":
		return 5
	case "sp":
		return 6
	default:
		return 7
	}
}

func isZeroMavenNumber(value string) bool {
	return strings.Trim(value, "0") == ""
}
