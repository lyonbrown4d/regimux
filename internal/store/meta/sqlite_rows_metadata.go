package meta

func endpointHealthRecordToRow(record EndpointHealthRecord) endpointHealthRow {
	return endpointHealthRow{
		ID:                   record.ID,
		Key:                  record.Key,
		Alias:                record.Alias,
		Registry:             record.Registry,
		Repository:           record.Repository,
		LatencyEWMA:          int64(record.LatencyEWMA),
		LatencySamples:       int64(record.LatencySamples),
		ConsecutiveFailures:  int64(record.ConsecutiveFailures),
		SuccessCount:         record.SuccessCount,
		FailureCount:         record.FailureCount,
		ContentMismatchCount: record.ContentMismatchCount,
		CooldownUntil:        unixNano(record.CooldownUntil),
		DegradedUntil:        unixNano(record.DegradedUntil),
		LastSuccessAt:        unixNano(record.LastSuccessAt),
		LastFailureAt:        unixNano(record.LastFailureAt),
		LastProbeAt:          unixNano(record.LastProbeAt),
		CreatedAt:            unixNano(record.CreatedAt),
		UpdatedAt:            unixNano(record.UpdatedAt),
	}
}

func endpointHealthRowToRecord(row endpointHealthRow) *EndpointHealthRecord {
	return &EndpointHealthRecord{
		ID:                   row.ID,
		Key:                  row.Key,
		Alias:                row.Alias,
		Registry:             row.Registry,
		Repository:           row.Repository,
		LatencyEWMA:          durationFromInt64(row.LatencyEWMA),
		LatencySamples:       intFromInt64(row.LatencySamples),
		ConsecutiveFailures:  intFromInt64(row.ConsecutiveFailures),
		SuccessCount:         row.SuccessCount,
		FailureCount:         row.FailureCount,
		ContentMismatchCount: row.ContentMismatchCount,
		CooldownUntil:        timeFromUnixNano(row.CooldownUntil),
		DegradedUntil:        timeFromUnixNano(row.DegradedUntil),
		LastSuccessAt:        timeFromUnixNano(row.LastSuccessAt),
		LastFailureAt:        timeFromUnixNano(row.LastFailureAt),
		LastProbeAt:          timeFromUnixNano(row.LastProbeAt),
		CreatedAt:            timeFromUnixNano(row.CreatedAt),
		UpdatedAt:            timeFromUnixNano(row.UpdatedAt),
	}
}

func upstreamRecordToRow(record Upstream) upstreamRow {
	return upstreamRow{
		ID:              record.ID,
		Alias:           record.Alias,
		RepositoryCount: record.RepositoryCount,
		PullCount:       record.PullCount,
		BlobBytes:       record.BlobBytes,
		BlobLinkCount:   record.BlobLinkCount,
		LastActivityAt:  unixNano(record.LastActivityAt),
		CreatedAt:       unixNano(record.CreatedAt),
		UpdatedAt:       unixNano(record.UpdatedAt),
	}
}

func upstreamRowToRecord(row upstreamRow) *Upstream {
	return &Upstream{
		ID:              row.ID,
		Alias:           row.Alias,
		RepositoryCount: row.RepositoryCount,
		PullCount:       row.PullCount,
		BlobBytes:       row.BlobBytes,
		BlobLinkCount:   row.BlobLinkCount,
		LastActivityAt:  timeFromUnixNano(row.LastActivityAt),
		CreatedAt:       timeFromUnixNano(row.CreatedAt),
		UpdatedAt:       timeFromUnixNano(row.UpdatedAt),
	}
}

func repositoryRecordToRow(record Repository) repositoryRow {
	return repositoryRow{
		ID:               record.ID,
		Key:              repositoryMetadataKey(record.Alias, record.Name),
		UpstreamID:       record.UpstreamID,
		Alias:            record.Alias,
		Name:             record.Name,
		PullCount:        record.PullCount,
		BlobBytes:        record.BlobBytes,
		BlobLinkCount:    record.BlobLinkCount,
		LastPullAt:       unixNano(record.LastPullAt),
		LastBlobAccessAt: unixNano(record.LastBlobAccessAt),
		LastActivityAt:   unixNano(record.LastActivityAt),
		CreatedAt:        unixNano(record.CreatedAt),
		UpdatedAt:        unixNano(record.UpdatedAt),
	}
}

func repositoryRowToRecord(row repositoryRow) *Repository {
	return &Repository{
		ID:               row.ID,
		UpstreamID:       row.UpstreamID,
		Alias:            row.Alias,
		Name:             row.Name,
		PullCount:        row.PullCount,
		BlobBytes:        row.BlobBytes,
		BlobLinkCount:    row.BlobLinkCount,
		LastPullAt:       timeFromUnixNano(row.LastPullAt),
		LastBlobAccessAt: timeFromUnixNano(row.LastBlobAccessAt),
		LastActivityAt:   timeFromUnixNano(row.LastActivityAt),
		CreatedAt:        timeFromUnixNano(row.CreatedAt),
		UpdatedAt:        timeFromUnixNano(row.UpdatedAt),
	}
}
