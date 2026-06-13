package meta

import (
	"strings"
	"time"

	ocidigest "github.com/opencontainers/go-digest"
)

type RefreshIntentEcosystem string

type RefreshIntentKind string

type RefreshIntentKey struct {
	Ecosystem  RefreshIntentEcosystem `json:"ecosystem"`
	Kind       RefreshIntentKind      `json:"kind,omitempty"`
	Alias      string                 `json:"alias"`
	Repository string                 `json:"repository"`
	Reference  string                 `json:"reference"`
	Accept     string                 `json:"accept,omitempty"`
}

type RefreshIntentRecord struct {
	ID         int64                  `json:"id,omitempty"`
	Key        string                 `json:"key,omitempty"`
	Ecosystem  RefreshIntentEcosystem `json:"ecosystem"`
	Kind       RefreshIntentKind      `json:"kind,omitempty"`
	Alias      string                 `json:"alias"`
	Repository string                 `json:"repository"`
	Reference  string                 `json:"reference"`
	Accept     string                 `json:"accept,omitempty"`
	DueAt      time.Time              `json:"due_at,omitzero"`
	LastSeenAt time.Time              `json:"last_seen_at,omitzero"`
	Skipped    int                    `json:"skipped"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

func (k RefreshIntentKey) String() string {
	parts := []string{
		strings.TrimSpace(string(k.Ecosystem)),
		strings.TrimSpace(string(k.Kind)),
		strings.TrimSpace(k.Alias),
		strings.TrimSpace(k.Repository),
		strings.TrimSpace(k.Reference),
		strings.TrimSpace(k.Accept),
	}
	return ocidigest.SHA256.FromBytes([]byte(strings.Join(parts, "\x00"))).Encoded()
}
