package upstream

import (
	"encoding/json"
	"io"
	"strings"
)

type bearerChallenge struct {
	Realm   string
	Service string
	Scope   string
}

func parseBearerChallenge(header string) bearerChallenge {
	header = strings.TrimSpace(header)
	if header == "" {
		return bearerChallenge{}
	}
	scheme, params, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return bearerChallenge{}
	}

	out := bearerChallenge{}
	for _, part := range splitChallengeParams(params) {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "realm":
			out.Realm = value
		case "service":
			out.Service = value
		case "scope":
			out.Scope = value
		}
	}
	return out
}

func splitChallengeParams(params string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	for _, r := range params {
		switch r {
		case '"':
			inQuote = !inQuote
			current.WriteRune(r)
		case ',':
			if inQuote {
				current.WriteRune(r)
				continue
			}
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}
	return parts
}

func decodeJSON(r io.Reader, out any) error {
	decoder := json.NewDecoder(r)
	return decoder.Decode(out)
}

