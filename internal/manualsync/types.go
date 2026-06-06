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

// SyncOptions identifies one artifact reference to prefetch explicitly.
type SyncOptions struct {
	Ecosystem string
	Alias     string
	Repo      string
	Reference string
	Accept    string
}

// SyncReport summarizes artifacts warmed by a manual sync.
type SyncReport struct {
	Alias              string
	Repo               string
	Reference          string
	ManifestDigest     string
	MediaType          string
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
