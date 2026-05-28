package admin

import (
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func cacheSummary(snapshot metadataSnapshot, now time.Time) CacheSummary {
	blobBytes := blobBytes(snapshot.blobs)
	return CacheSummary{
		ManifestCount:        len(snapshot.manifests),
		ExpiredManifestCount: expiredManifestCount(snapshot.manifests, now),
		TagCount:             len(snapshot.tags),
		ExpiredTagCount:      expiredTagCount(snapshot.tags, now),
		BlobCount:            len(snapshot.blobs),
		BlobBytes:            formatBytes(blobBytes),
		RepoBlobCount:        len(snapshot.repoBlobs),
		RecentBlobs:          recentBlobRows(snapshot.blobs, 25),
	}
}

func expiredManifestCount(records []meta.ManifestRecord, now time.Time) int {
	count := 0
	for i := range records {
		record := &records[i]
		if record.Expired(now) {
			count++
		}
	}
	return count
}

func expiredTagCount(records []meta.TagRecord, now time.Time) int {
	count := 0
	for i := range records {
		record := &records[i]
		if !record.ExpiresAt.IsZero() && !now.Before(record.ExpiresAt) {
			count++
		}
	}
	return count
}

func blobBytes(records []meta.BlobRecord) int64 {
	var total int64
	for i := range records {
		record := &records[i]
		if record.Size > 0 {
			total += record.Size
		}
	}
	return total
}

func recentBlobRows(records []meta.BlobRecord, limit int) []BlobRow {
	sorted := collectionlist.NewList(records...).Sort(compareBlobRecordRecent)
	out := collectionlist.NewListWithCapacity[BlobRow](min(limit, sorted.Len()))
	sorted.Range(func(index int, record meta.BlobRecord) bool {
		if index >= limit {
			return false
		}
		out.Add(BlobRow{
			Digest:       record.Digest,
			Size:         formatBytes(record.Size),
			MediaType:    dash(record.MediaType),
			LastAccessAt: formatTime(record.LastAccessAt),
			UpdatedAt:    formatTime(record.UpdatedAt),
		})
		return true
	})
	return out.Values()
}

func compareBlobRecordRecent(left, right meta.BlobRecord) int {
	leftTime := latestTime(left.LastAccessAt, left.UpdatedAt, left.CreatedAt)
	rightTime := latestTime(right.LastAccessAt, right.UpdatedAt, right.CreatedAt)
	return compareTimeDesc(leftTime, rightTime)
}
