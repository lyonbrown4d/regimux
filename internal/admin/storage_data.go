package admin

func (s *Service) storageSummary(snapshot metadataSnapshot) (StorageSummary, error) {
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
		RecentBlobs:   recentBlobs,
		LargeBlobs:    largeBlobs,
		Repositories:  repositories,
		RepoBlobLinks: repoBlobLinks,
	}, nil
}
