package reference

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// NormalizeAccept canonicalizes an Accept header for use in cache keys.
// It preserves media-range order because order can affect content negotiation.
func NormalizeAccept(header string) string {
	parts := splitHeaderList(header)
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		item := normalizeAcceptItem(part)
		if item != "" {
			normalized = append(normalized, item)
		}
	}
	return strings.Join(normalized, ",")
}

// AcceptKey returns a stable sha256 hex key for a normalized Accept header.
func AcceptKey(header string) string {
	sum := sha256.Sum256([]byte(NormalizeAccept(header)))
	return hex.EncodeToString(sum[:])
}

func normalizeAcceptItem(item string) string {
	pieces := splitSemicolonList(item)
	if len(pieces) == 0 {
		return ""
	}

	mediaType := strings.ToLower(strings.TrimSpace(pieces[0]))
	if mediaType == "" {
		return ""
	}

	params := make([]string, 0, len(pieces)-1)
	for _, raw := range pieces[1:] {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		name, value, ok := strings.Cut(raw, "=")
		if !ok {
			params = append(params, strings.ToLower(raw))
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		value = strings.TrimSpace(value)
		if name == "" {
			continue
		}
		if name == "q" && (value == "1" || value == "1.0" || value == "1.00" || value == "1.000") {
			continue
		}
		params = append(params, name+"="+value)
	}
	sort.Strings(params)

	if len(params) == 0 {
		return mediaType
	}
	return mediaType + ";" + strings.Join(params, ";")
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
