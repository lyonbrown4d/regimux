package admin

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func (s *Service) storageSummary(ctx context.Context, snapshot metadataSnapshot, includeObjectStore bool) (StorageSummary, error) {
	stats := snapshot.stats
	recentBlobs, err := s.mapper.BlobRows(snapshot.recentBlobs, 10)
	if err != nil {
		return StorageSummary{}, err
	}
	largeBlobs, err := s.mapper.BlobRows(snapshot.largeBlobs, 10)
	if err != nil {
		return StorageSummary{}, err
	}
	repositories, err := s.mapper.RepositoryRows(snapshot.repositories, 25)
	if err != nil {
		return StorageSummary{}, err
	}
	repoBlobLinks, err := s.mapper.RepoBlobRows(snapshot.repoBlobs, 25)
	if err != nil {
		return StorageSummary{}, err
	}
	return StorageSummary{
		TotalBytes:    formatBytes(stats.BlobBytes + stats.ManifestBytes),
		BlobBytes:     formatBytes(stats.BlobBytes),
		ManifestBytes: formatBytes(stats.ManifestBytes),
		BlobCount:     metadataCount(stats.BlobCount),
		ManifestCount: metadataCount(stats.ManifestCount),
		RepoBlobCount: metadataCount(stats.RepoBlobCount),
		ObjectStore:   s.objectStoreSummary(ctx, includeObjectStore),
		RecentBlobs:   recentBlobs,
		LargeBlobs:    largeBlobs,
		Repositories:  repositories,
		RepoBlobLinks: repoBlobLinks,
	}, nil
}

func (s *Service) objectStoreSummary(ctx context.Context, include bool) ObjectStoreSummary {
	if !include {
		return ObjectStoreSummary{Bytes: "-"}
	}
	if s == nil || s.objects == nil {
		return ObjectStoreSummary{Bytes: "-", Error: "object store is not configured"}
	}
	walker, ok := s.objects.(object.ObjectWalker)
	if !ok {
		return ObjectStoreSummary{Bytes: "-", Error: "object store list is not supported"}
	}
	var count int64
	var bytes int64
	if err := walker.WalkObjects(ctx, func(info object.Info) error {
		count++
		if info.Size > 0 {
			bytes += info.Size
		}
		return nil
	}); err != nil {
		return ObjectStoreSummary{Bytes: "-", Error: err.Error()}
	}
	return ObjectStoreSummary{
		Available: true,
		Count:     metadataCount(count),
		Bytes:     formatBytes(bytes),
	}
}
