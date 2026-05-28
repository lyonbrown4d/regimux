package admin

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func storageSummary(snapshot metadataSnapshot) StorageSummary {
	blobs := blobBytes(snapshot.blobs)
	manifests := manifestBytes(snapshot.manifests)
	return StorageSummary{
		TotalBytes:    formatBytes(blobs + manifests),
		BlobBytes:     formatBytes(blobs),
		ManifestBytes: formatBytes(manifests),
		BlobCount:     len(snapshot.blobs),
		ManifestCount: len(snapshot.manifests),
		RepoBlobCount: len(snapshot.repoBlobs),
		RecentBlobs:   recentBlobRows(snapshot.blobs, 10),
		LargeBlobs:    largeBlobRows(snapshot.blobs, 10),
		RepoBlobLinks: recentRepoBlobRows(snapshot.repoBlobs, 25),
	}
}

func manifestBytes(records []meta.ManifestRecord) int64 {
	var total int64
	for i := range records {
		record := &records[i]
		if record.Size > 0 {
			total += record.Size
		}
	}
	return total
}

func largeBlobRows(records []meta.BlobRecord, limit int) []BlobRow {
	sorted := collectionlist.NewList(records...).Sort(compareBlobRecordSizeDesc)
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

func recentRepoBlobRows(records []meta.RepoBlobRecord, limit int) []RepoBlobRow {
	sorted := collectionlist.NewList(records...).Sort(compareRepoBlobRecordRecent)
	out := collectionlist.NewListWithCapacity[RepoBlobRow](min(limit, sorted.Len()))
	sorted.Range(func(index int, record meta.RepoBlobRecord) bool {
		if index >= limit {
			return false
		}
		out.Add(RepoBlobRow{
			Key:            record.Key,
			Alias:          record.Alias,
			Repository:     record.Repository,
			Digest:         record.Digest,
			SourceManifest: dash(record.SourceManifest),
			LastAccessAt:   formatTime(record.LastAccessAt),
			LastVerifiedAt: formatTime(record.LastVerifiedAt),
			UpdatedAt:      formatTime(record.UpdatedAt),
		})
		return true
	})
	return out.Values()
}

func compareBlobRecordSizeDesc(left, right meta.BlobRecord) int {
	switch {
	case left.Size == right.Size:
		return compareBlobRecordRecent(left, right)
	case left.Size > right.Size:
		return -1
	default:
		return 1
	}
}

func compareRepoBlobRecordRecent(left, right meta.RepoBlobRecord) int {
	leftTime := latestTime(left.LastAccessAt, left.LastVerifiedAt, left.UpdatedAt, left.CreatedAt)
	rightTime := latestTime(right.LastAccessAt, right.LastVerifiedAt, right.UpdatedAt, right.CreatedAt)
	return compareTimeDesc(leftTime, rightTime)
}
