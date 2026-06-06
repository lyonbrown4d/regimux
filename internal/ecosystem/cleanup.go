package ecosystem

import "context"

// CleanupReport summarizes cache cleanup for any ecosystem.
type CleanupReport struct {
	Ecosystem              string
	DryRun                 bool
	ScannedBlobs           int
	RecentBlobs            int
	MissingAccessTimeBlobs int
	ProtectedBlobs         int
	EligibleBlobs          int
	DeletedBlobs           int
	MissingObjects         int
	BytesBefore            int64
	BytesAfter             int64
	BytesTarget            int64
	BytesDeleted           int64
	CapacityExceeded       bool
	LimitReached           bool
}

// Cleaner is implemented by ecosystems that can reclaim cached artifacts.
type Cleaner interface {
	Runtime
	Cleanup(context.Context) (*CleanupReport, error)
}
