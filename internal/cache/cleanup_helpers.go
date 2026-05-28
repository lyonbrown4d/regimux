package cache

import (
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionset "github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func newCleanupReport(opts CleanupOptions, blobs []meta.BlobRecord) *CleanupReport {
	report := &CleanupReport{
		DryRun:      opts.DryRun,
		BytesBefore: cleanupBlobBytes(blobs),
	}
	report.BytesAfter = report.BytesBefore
	report.BytesTarget = cleanupCapacityTarget(opts)
	report.CapacityExceeded = opts.MaxBytes > 0 && report.BytesBefore > opts.MaxBytes
	return report
}

func cleanupBlobBytes(blobs []meta.BlobRecord) int64 {
	var total int64
	for i := range blobs {
		if blobs[i].Size > 0 {
			total += blobs[i].Size
		}
	}
	return total
}

func cleanupCapacityTarget(opts CleanupOptions) int64 {
	if opts.MaxBytes <= 0 {
		return 0
	}
	if opts.TargetBytes > 0 {
		return opts.TargetBytes
	}
	return opts.MaxBytes
}

func cleanupOrderedBlobs(blobs []meta.BlobRecord) []meta.BlobRecord {
	return collectionlist.NewList(blobs...).Sort(compareCleanupBlob).Values()
}

func compareCleanupBlob(left, right meta.BlobRecord) int {
	if out := compareCleanupAccessTime(left.LastAccessAt, right.LastAccessAt); out != 0 {
		return out
	}
	if left.Size < right.Size {
		return -1
	}
	if left.Size > right.Size {
		return 1
	}
	if left.Digest < right.Digest {
		return -1
	}
	if left.Digest > right.Digest {
		return 1
	}
	return 0
}

func compareCleanupAccessTime(left, right time.Time) int {
	switch {
	case left.IsZero() && right.IsZero():
		return 0
	case left.IsZero():
		return 1
	case right.IsZero():
		return -1
	case left.Before(right):
		return -1
	case left.After(right):
		return 1
	default:
		return 0
	}
}

func (r *CleanupReport) needsCapacityReclaim() bool {
	return r != nil && r.CapacityExceeded && r.BytesTarget >= 0 && r.BytesAfter > r.BytesTarget
}

func cleanupScanLimitReached(opts CleanupOptions, report *CleanupReport) bool {
	if opts.MaxScan <= 0 || report.ScannedBlobs < opts.MaxScan {
		return false
	}
	report.LimitReached = true
	return true
}

func cleanupDeleteLimitReached(opts CleanupOptions, report *CleanupReport) bool {
	if opts.MaxDeletes <= 0 || report.DeletedBlobs < opts.MaxDeletes {
		return false
	}
	report.LimitReached = true
	return true
}

func classifyCleanupBlob(blob *meta.BlobRecord, cutoff time.Time, protected *collectionset.Set[string]) cleanupSkipReason {
	if blob == nil || blob.LastAccessAt.IsZero() {
		return cleanupMissingAccessTime
	}
	if !blob.LastAccessAt.Before(cutoff) {
		return cleanupRecent
	}
	if protected.Contains(blob.Digest) {
		return cleanupProtected
	}
	return cleanupEligible
}

func (r *CleanupReport) recordCleanupSkip(reason cleanupSkipReason) {
	switch reason {
	case cleanupMissingAccessTime:
		r.MissingAccessTimeBlobs++
	case cleanupRecent:
		r.RecentBlobs++
	case cleanupProtected:
		r.ProtectedBlobs++
	case cleanupEligible:
		return
	}
}

func cleanupRemainingBytes(current, deleted int64) int64 {
	if deleted <= 0 {
		return current
	}
	if deleted >= current {
		return 0
	}
	return current - deleted
}
