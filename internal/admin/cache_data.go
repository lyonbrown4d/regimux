package admin

func (s *Service) cacheSummary(snapshot metadataSnapshot) (CacheSummary, error) {
	stats := snapshot.stats
	recentBlobs, err := s.mapper.BlobRows(snapshot.recentBlobs, 25)
	if err != nil {
		return CacheSummary{}, err
	}
	return CacheSummary{
		ManifestCount:        metadataCount(stats.ManifestCount),
		ExpiredManifestCount: metadataCount(stats.ExpiredManifestCount),
		TagCount:             metadataCount(stats.TagCount),
		ExpiredTagCount:      metadataCount(stats.ExpiredTagCount),
		BlobCount:            metadataCount(stats.BlobCount),
		BlobBytes:            formatBytes(stats.BlobBytes),
		RepoBlobCount:        metadataCount(stats.RepoBlobCount),
		RecentBlobs:          recentBlobs,
	}, nil
}
