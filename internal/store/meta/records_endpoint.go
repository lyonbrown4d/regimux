package meta

import (
	"time"
)

type EndpointHealthKey struct {
	Alias      string `json:"alias"`
	Registry   string `json:"registry"`
	Repository string `json:"repository,omitempty"`
}

type EndpointHealthRecord struct {
	ID                   int64         `json:"id,omitempty"`
	Key                  string        `json:"key,omitempty"`
	Alias                string        `json:"alias"`
	Registry             string        `json:"registry"`
	Repository           string        `json:"repository,omitempty"`
	LatencyEWMA          time.Duration `json:"latency_ewma,omitempty"`
	LatencySamples       int           `json:"latency_samples,omitempty"`
	ConsecutiveFailures  int           `json:"consecutive_failures,omitempty"`
	SuccessCount         int64         `json:"success_count,omitempty"`
	FailureCount         int64         `json:"failure_count,omitempty"`
	ContentMismatchCount int64         `json:"content_mismatch_count,omitempty"`
	CooldownUntil        time.Time     `json:"cooldown_until,omitzero"`
	DegradedUntil        time.Time     `json:"degraded_until,omitzero"`
	LastSuccessAt        time.Time     `json:"last_success_at,omitzero"`
	LastFailureAt        time.Time     `json:"last_failure_at,omitzero"`
	LastProbeAt          time.Time     `json:"last_probe_at,omitzero"`
	CreatedAt            time.Time     `json:"created_at"`
	UpdatedAt            time.Time     `json:"updated_at"`
}

func (k EndpointHealthKey) String() string {
	return k.Alias + "\x1f" + k.Registry + "\x1f" + k.Repository
}
