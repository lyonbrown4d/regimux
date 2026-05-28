package admin

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func cacheSummary(snapshot metadataSnapshot) CacheSummary {
	stats := snapshot.stats
	return CacheSummary{
		ManifestCount:        metadataCount(stats.ManifestCount),
		ExpiredManifestCount: metadataCount(stats.ExpiredManifestCount),
		TagCount:             metadataCount(stats.TagCount),
		ExpiredTagCount:      metadataCount(stats.ExpiredTagCount),
		BlobCount:            metadataCount(stats.BlobCount),
		BlobBytes:            formatBytes(stats.BlobBytes),
		RepoBlobCount:        metadataCount(stats.RepoBlobCount),
		RecentBlobs:          recentBlobRows(snapshot.recentBlobs, 25).Values(),
	}
}

func recentBlobRows(records *collectionlist.List[meta.BlobRecord], limit int) *collectionlist.List[BlobRow] {
	if records == nil {
		return collectionlist.NewList[BlobRow]()
	}
	return collectionlist.MapList(records.Take(limit), func(_ int, record meta.BlobRecord) BlobRow {
		return BlobRow{
			Digest:       record.Digest,
			Size:         formatBytes(record.Size),
			MediaType:    dash(record.MediaType),
			LastAccessAt: formatTime(record.LastAccessAt),
			UpdatedAt:    formatTime(record.UpdatedAt),
		}
	})
}
