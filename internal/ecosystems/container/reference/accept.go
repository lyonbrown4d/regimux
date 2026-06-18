// Package reference parses registry references and HTTP selectors.
package reference

import (
	"maps"
	"mime"
	"slices"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	ocidigest "github.com/opencontainers/go-digest"
)

// NormalizeAccept canonicalizes an Accept header for use in cache keys.
// It preserves media-range order because order can affect content negotiation.
func NormalizeAccept(header string) string {
	items := collectionlist.NewList[string]()
	for _, part := range splitHeaderList(header) {
		item := normalizeAcceptItem(part)
		if item != "" {
			items.Add(item)
		}
	}
	return strings.Join(items.Values(), ",")
}

// AcceptKey returns a stable sha256 hex key for a normalized Accept header.
func AcceptKey(header string) string {
	return ocidigest.SHA256.FromBytes([]byte(NormalizeAccept(header))).Encoded()
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
	clean := collectionlist.NewList[string]()
	for _, raw := range rawParams {
		if param, ok := normalizeAcceptParam(raw); ok {
			clean.Add(param)
		}
	}
	return clean.Sort(strings.Compare)
}

func normalizeAcceptParamMap(params map[string]string) *collectionlist.List[string] {
	clean := collectionlist.NewList[string]()
	for _, name := range slices.Sorted(maps.Keys(params)) {
		value := params[name]
		param, ok := normalizeAcceptParam(name + "=" + value)
		if ok {
			clean.Add(param)
		}
	}
	return clean.Sort(strings.Compare)
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
