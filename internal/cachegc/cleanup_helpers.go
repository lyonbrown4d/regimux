package cachegc

import (
	"time"

	"github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func newReport(opts CleanupOptions, blobs *list.List[meta.BlobRecord]) *CleanupReport {
	r := &CleanupReport{DryRun: opts.DryRun, BytesTarget: opts.MaxBytes}
	if opts.TargetBytes > 0 {
		r.BytesTarget = opts.TargetBytes
	}
	if blobs != nil {
		blobs.Range(func(_ int, b meta.BlobRecord) bool {
			if opts.Candidates != nil && !opts.Candidates[b.Digest] {
				return true
			}
			if b.Size > 0 {
				r.BytesBefore += b.Size
			}
			return true
		})
	}
	r.BytesAfter = r.BytesBefore
	r.CapacityExceeded = opts.MaxBytes > 0 && r.BytesBefore > opts.MaxBytes
	return r
}
func orderBlobs(blobs *list.List[meta.BlobRecord], capacity bool) *list.List[meta.BlobRecord] {
	if blobs == nil {
		return list.NewList[meta.BlobRecord]()
	}
	if capacity {
		return blobs.Sort(compareCapacity)
	}
	return blobs.Sort(compareAge)
}
func compareCapacity(a, b meta.BlobRecord) int {
	if a.LastAccessAt.IsZero() != b.LastAccessAt.IsZero() {
		if a.LastAccessAt.IsZero() {
			return -1
		}
		return 1
	}
	if a.Size > b.Size {
		return -1
	}
	if a.Size < b.Size {
		return 1
	}
	return cmpTime(a.LastAccessAt, b.LastAccessAt)
}
func compareAge(a, b meta.BlobRecord) int {
	if c := cmpTime(a.LastAccessAt, b.LastAccessAt); c != 0 {
		return c
	}
	if a.Size < b.Size {
		return -1
	}
	if a.Size > b.Size {
		return 1
	}
	return cmpString(a.Digest, b.Digest)
}
func cmpTime(a, b time.Time) int {
	if a.IsZero() {
		if b.IsZero() {
			return 0
		}
		return 1
	}
	if b.IsZero() {
		return -1
	}
	if a.Before(b) {
		return -1
	}
	if a.After(b) {
		return 1
	}
	return 0
}
func cmpString(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
func classify(b meta.BlobRecord, cutoff time.Time, protected *set.Set[string], prune bool) skipReason {
	if protected.Contains(b.Digest) {
		return reasonProtected
	}
	if !prune {
		if b.LastAccessAt.IsZero() {
			return reasonMissingTime
		}
		if !b.LastAccessAt.Before(cutoff) {
			return reasonRecent
		}
		return reasonEligible
	}
	if b.LastAccessAt.IsZero() && b.UpdatedAt.IsZero() && b.CreatedAt.IsZero() {
		return reasonMissingTime
	}
	if isRecent(cutoff, b.LastAccessAt, b.UpdatedAt, b.CreatedAt) {
		return reasonRecent
	}
	return reasonEligible
}
func recordSkip(r *CleanupReport, reason skipReason) {
	switch reason {
	case reasonEligible:
		return
	case reasonMissingTime:
		r.MissingAccessTimeBlobs++
	case reasonRecent:
		r.RecentBlobs++
	case reasonProtected:
		r.ProtectedBlobs++
	}
}
func planDelete(r *CleanupReport, b *meta.BlobRecord) {
	r.DeletedBlobs++
	r.DeletedDigests = append(r.DeletedDigests, b.Digest)
	if b.Size > 0 {
		r.BytesDeleted += b.Size
		r.BytesAfter = remaining(r.BytesAfter, b.Size)
	}
}
func remaining(a, b int64) int64 {
	if b <= 0 {
		return a
	}
	if b >= a {
		return 0
	}
	return a - b
}
func limitReached(limit, current int) bool { return limit > 0 && current >= limit }
