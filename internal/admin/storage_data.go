package admin

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func storageSummary(snapshot metadataSnapshot) StorageSummary {
	stats := snapshot.stats
	return StorageSummary{
		TotalBytes:    formatBytes(stats.BlobBytes + stats.ManifestBytes),
		BlobBytes:     formatBytes(stats.BlobBytes),
		ManifestBytes: formatBytes(stats.ManifestBytes),
		BlobCount:     metadataCount(stats.BlobCount),
		ManifestCount: metadataCount(stats.ManifestCount),
		RepoBlobCount: metadataCount(stats.RepoBlobCount),
		RecentBlobs:   recentBlobRows(snapshot.recentBlobs, 10).Values(),
		LargeBlobs:    largeBlobRows(snapshot.largeBlobs, 10).Values(),
		Repositories:  repositoryRows(snapshot.repositories, 25).Values(),
		RepoBlobLinks: recentRepoBlobRows(snapshot.repoBlobs, 25).Values(),
	}
}

func largeBlobRows(records *collectionlist.List[meta.BlobRecord], limit int) *collectionlist.List[BlobRow] {
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

func recentRepoBlobRows(records *collectionlist.List[meta.RepoBlobRecord], limit int) *collectionlist.List[RepoBlobRow] {
	if records == nil {
		return collectionlist.NewList[RepoBlobRow]()
	}
	return collectionlist.MapList(records.Take(limit), func(_ int, record meta.RepoBlobRecord) RepoBlobRow {
		return RepoBlobRow{
			Key:            record.Key,
			Alias:          record.Alias,
			Repository:     record.Repository,
			Digest:         record.Digest,
			SourceManifest: dash(record.SourceManifest),
			LastAccessAt:   formatTime(record.LastAccessAt),
			LastVerifiedAt: formatTime(record.LastVerifiedAt),
			UpdatedAt:      formatTime(record.UpdatedAt),
		}
	})
}

func repositoryRows(records *collectionlist.List[meta.Repository], limit int) *collectionlist.List[RepositoryRow] {
	if records == nil {
		return collectionlist.NewList[RepositoryRow]()
	}
	return collectionlist.MapList(records.Take(limit), func(_ int, record meta.Repository) RepositoryRow {
		return RepositoryRow{
			Alias:            record.Alias,
			Repository:       record.Name,
			PullCount:        record.PullCount,
			BlobBytes:        formatBytes(record.BlobBytes),
			BlobLinkCount:    record.BlobLinkCount,
			LastPullAt:       formatTime(record.LastPullAt),
			LastBlobAccessAt: formatTime(record.LastBlobAccessAt),
			LastActivityAt:   formatTime(record.LastActivityAt),
		}
	})
}
