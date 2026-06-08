package manualsync

import (
	"time"
)

const (
	SyncJobStatusQueued    = "queued"
	SyncJobStatusRunning   = "running"
	SyncJobStatusSucceeded = "succeeded"
	SyncJobStatusFailed    = "failed"
)

// SyncOptions identifies one ecosystem artifact reference to warm explicitly.
type SyncOptions struct {
	Ecosystem string
	Alias     string
	Artifact  string
	Reference string
	Accept    string
}

// SyncReport summarizes the artifact data warmed by a manual sync.
type SyncReport struct {
	Alias              string
	Artifact           string
	Reference          string
	Digest             string
	MediaType          string
	BytesWarmed        int64
	LayerCount         int
	BlobCount          int
	ChildManifestCount int
	Duration           time.Duration
}

type SyncJob struct {
	ID         string
	Status     string
	Options    SyncOptions
	Result     *SyncReport
	Error      string
	CreatedAt  time.Time
	StartedAt  time.Time
	FinishedAt time.Time
}
