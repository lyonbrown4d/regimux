// Package reference parses registry references and HTTP selectors.
package reference

import (
	"crypto/sha256"
	"encoding/hex"
	"mime"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/samber/lo"
)

// NormalizeAccept canonicalizes an Accept header for use in cache keys.
// It preserves media-range order because order can affect content negotiation.
func NormalizeAccept(header string) string {
	return strings.Join(lo.FilterMap(splitHeaderList(header), func(part string, _ int) (string, bool) {
		item := normalizeAcceptItem(part)
		return item, item != ""
	}), ",")
}

// AcceptKey returns a stable sha256 hex key for a normalized Accept header.
func AcceptKey(header string) string {
	sum := sha256.Sum256([]byte(NormalizeAccept(header)))
	return hex.EncodeToString(sum[:])
}

func normalizeAcceptItem(item string) string {
	item = strings.TrimSpace(item)
	if item == "" {
		return ""
	}

	mediaType, params, err := mime.ParseMediaType(item)
	if err != nil {
		pieces := splitSemicolonList(item)
		if len(pieces) == 0 {
			return ""
		}
		mediaType = normalizeMediaType(pieces[0])
		if mediaType == "" {
			return ""
		}
		fallbackParams := normalizeAcceptParams(pieces[1:])
		if fallbackParams.IsEmpty() {
			return mediaType
		}
		return mediaType + ";" + fallbackParams.Join(";")
	}
	normalized := normalizeMediaType(mediaType)
	if normalized == "" {
		return ""
	}
	paramPairs := normalizeAcceptParamMap(params)
	if paramPairs.IsEmpty() {
		return normalized
	}
	return normalized + ";" + paramPairs.Join(";")
}

func normalizeMediaType(value string) string {
	value = strings.TrimSpace(value)
	mediaType, _, err := mime.ParseMediaType(value)
	if err == nil && mediaType != "" {
		return mediaType
	}
	return strings.ToLower(value)
}

func normalizeAcceptParams(rawParams []string) *collectionlist.List[string] {
	return collectionlist.NewList(lo.FilterMap(rawParams, func(raw string, _ int) (string, bool) {
		return normalizeAcceptParam(raw)
	})...).Sort(strings.Compare)
}

func normalizeAcceptParamMap(params map[string]string) *collectionlist.List[string] {
	return collectionlist.NewList(lo.FilterMap(lo.Keys(params), func(name string, _ int) (string, bool) {
		value := params[name]
		param, ok := normalizeAcceptParam(name + "=" + value)
		return param, ok
	})...).Sort(strings.Compare)
}

func normalizeAcceptParam(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	name, value, ok := strings.Cut(raw, "=")
	if !ok {
		return strings.ToLower(raw), true
	}
	name = strings.ToLower(strings.TrimSpace(name))
	value = strings.TrimSpace(value)
	if name == "" || isDefaultQuality(name, value) {
		return "", false
	}
	return name + "=" + value, true
}

func isDefaultQuality(name, value string) bool {
	if name != "q" {
		return false
	}
	switch value {
	case "1", "1.0", "1.00", "1.000":
		return true
	default:
		return false
	}
}

func splitHeaderList(value string) []string {
	return splitQuoted(value, ',')
}

func splitSemicolonList(value string) []string {
	return splitQuoted(value, ';')
}

func splitQuoted(value string, sep rune) []string {
	var parts []string
	start := 0
	inQuote := false
	escaped := false
	for i, r := range value {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = inQuote
		case r == '"':
			inQuote = !inQuote
		case r == sep && !inQuote:
			parts = append(parts, strings.TrimSpace(value[start:i]))
			start = i + 1
		}
	}
	parts = append(parts, strings.TrimSpace(value[start:]))
	return parts
}
