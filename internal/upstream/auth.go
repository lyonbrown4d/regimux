// Package upstream contains upstream registry client integrations.
package upstream

import (
	"encoding/json"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
)

const defaultBearerTokenTTL = 5 * time.Minute

type bearerChallenge struct {
	Realm   string
	Service string
	Scope   string
}

type bearerTokenRequest struct {
	URL      string
	CacheKey bearerTokenCacheKey
}

type bearerTokenCacheKey struct {
	Endpoint string
	Realm    string
	Service  string
	Scope    string
	Username string
}

type bearerTokenCacheEntry struct {
	Token     string
	ExpiresAt time.Time
}

type bearerTokenCache struct {
	mu      sync.Mutex
	entries *collectionmapping.Map[bearerTokenCacheKey, bearerTokenCacheEntry]
}

type bearerTokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	IssuedAt    string `json:"issued_at"`
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

func newBearerTokenCache() *bearerTokenCache {
	return &bearerTokenCache{
		entries: collectionmapping.NewMap[bearerTokenCacheKey, bearerTokenCacheEntry](),
	}
}

func (c *bearerTokenCache) get(key bearerTokenCacheKey) (string, bool) {
	if c == nil {
		return "", false
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries.Get(key)
	if !ok {
		return "", false
	}
	if !entry.ExpiresAt.After(now) {
		c.entries.Delete(key)
		return "", false
	}
	return entry.Token, true
}

func (c *bearerTokenCache) set(key bearerTokenCacheKey, token string, expiresAt time.Time) {
	if c == nil || token == "" || !expiresAt.After(time.Now()) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries.Set(key, bearerTokenCacheEntry{Token: token, ExpiresAt: expiresAt})
}

func newBearerTokenRequest(cfg Config, challenge bearerChallenge, fallbackScope string) (bearerTokenRequest, error) {
	realm, err := url.Parse(challenge.Realm)
	if err != nil {
		return bearerTokenRequest{}, wrapError(err, "parse bearer token realm")
	}
	query := realm.Query()

	service := strings.TrimSpace(challenge.Service)
	if service != "" {
		query.Set("service", service)
	} else {
		service = strings.TrimSpace(query.Get("service"))
	}

	scope := strings.TrimSpace(challenge.Scope)
	if scope == "" {
		scope = strings.TrimSpace(fallbackScope)
	}
	if scope != "" {
		query.Set("scope", scope)
	} else {
		scope = strings.TrimSpace(query.Get("scope"))
	}

	realm.RawQuery = query.Encode()
	return bearerTokenRequest{
		URL: realm.String(),
		CacheKey: bearerTokenCacheKey{
			Endpoint: strings.TrimRight(strings.TrimSpace(cfg.Registry), "/"),
			Realm:    realm.String(),
			Service:  service,
			Scope:    scope,
			Username: cfg.Auth.Username,
		},
	}, nil
}

func bearerTokenExpiresAt(resp bearerTokenResponse) time.Time {
	ttl := defaultBearerTokenTTL
	if resp.ExpiresIn > 0 {
		ttl = time.Duration(resp.ExpiresIn) * time.Second
	}

	issuedAt := time.Now()
	if strings.TrimSpace(resp.IssuedAt) != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, resp.IssuedAt); err == nil {
			issuedAt = parsed
		}
	}
	return issuedAt.Add(ttl)
}

func splitChallengeParams(params string) []string {
	var parts []string
	inQuote := false
	start := 0
	for i, r := range params {
		switch r {
		case '"':
			inQuote = !inQuote
		case ',':
			if inQuote {
				continue
			}
			parts = append(parts, strings.TrimSpace(params[start:i]))
			start = i + 1
		}
	}
	if start < len(params) {
		parts = append(parts, strings.TrimSpace(params[start:]))
	}
	return parts
}

func decodeJSON(r io.Reader, out any) error {
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(out); err != nil {
		return wrapError(err, "decode upstream JSON response")
	}
	return nil
}
