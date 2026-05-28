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
	return collectionlist.ReduceList(collectionlist.NewList(records...), 0, func(count int, _ int, record meta.ManifestRecord) int {
		if record.Expired(now) {
			return count + 1
		}
		return count
	})
}

func expiredTagCount(records []meta.TagRecord, now time.Time) int {
	return collectionlist.ReduceList(collectionlist.NewList(records...), 0, func(count int, _ int, record meta.TagRecord) int {
		if !record.ExpiresAt.IsZero() && !now.Before(record.ExpiresAt) {
			return count + 1
		}
		return count
	})
}

func blobBytes(records []meta.BlobRecord) int64 {
	return collectionlist.ReduceList(collectionlist.NewList(records...), int64(0), func(total int64, _ int, record meta.BlobRecord) int64 {
		if record.Size > 0 {
			return total + record.Size
		}
		return total
	})
}

func recentBlobRows(records []meta.BlobRecord, limit int) []BlobRow {
	sorted := collectionlist.NewList(records...).Sort(compareBlobRecordRecent)
	return collectionlist.MapList(sorted.Take(limit), func(_ int, record meta.BlobRecord) BlobRow {
		return BlobRow{
			Digest:       record.Digest,
			Size:         formatBytes(record.Size),
			MediaType:    dash(record.MediaType),
			LastAccessAt: formatTime(record.LastAccessAt),
			UpdatedAt:    formatTime(record.UpdatedAt),
		}
	}).Values()
}

func compareBlobRecordRecent(left, right meta.BlobRecord) int {
	leftTime := latestTime(left.LastAccessAt, left.UpdatedAt, left.CreatedAt)
	rightTime := latestTime(right.LastAccessAt, right.UpdatedAt, right.CreatedAt)
	return compareTimeDesc(leftTime, rightTime)
}
