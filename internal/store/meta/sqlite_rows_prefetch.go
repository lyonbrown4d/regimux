package meta

func prefetchRunRecordToRow(record PrefetchRunRecord) prefetchRunRow {
	return prefetchRunRow{
		ID:                  record.ID,
		Status:              record.Status,
		Trigger:             record.Trigger,
		StartedAt:           unixNano(record.StartedAt),
		FinishedAt:          unixNano(record.FinishedAt),
		ScannedRecords:      record.ScannedRecords,
		SkippedRecords:      record.SkippedRecords,
		Repositories:        record.Repositories,
		SkippedRepositories: record.SkippedRepositories,
		Candidates:          record.Candidates,
		Prefetched:          record.Prefetched,
		Failed:              record.Failed,
		SkippedCandidates:   record.SkippedCandidates,
		BytesWarmed:         record.BytesWarmed,
		ByteBudget:          record.ByteBudget,
		TaskBudget:          record.TaskBudget,
		RepositoryLimit:     record.RepositoryLimit,
		RetryRequested:      record.RetryRequested,
		Error:               record.Error,
		CreatedAt:           unixNano(record.CreatedAt),
		UpdatedAt:           unixNano(record.UpdatedAt),
	}
}

func prefetchRunRowToRecord(row prefetchRunRow) *PrefetchRunRecord {
	return &PrefetchRunRecord{
		ID:                  row.ID,
		Status:              row.Status,
		Trigger:             row.Trigger,
		StartedAt:           timeFromUnixNano(row.StartedAt),
		FinishedAt:          timeFromUnixNano(row.FinishedAt),
		ScannedRecords:      row.ScannedRecords,
		SkippedRecords:      row.SkippedRecords,
		Repositories:        row.Repositories,
		SkippedRepositories: row.SkippedRepositories,
		Candidates:          row.Candidates,
		Prefetched:          row.Prefetched,
		Failed:              row.Failed,
		SkippedCandidates:   row.SkippedCandidates,
		BytesWarmed:         row.BytesWarmed,
		ByteBudget:          row.ByteBudget,
		TaskBudget:          row.TaskBudget,
		RepositoryLimit:     row.RepositoryLimit,
		RetryRequested:      row.RetryRequested,
		Error:               row.Error,
		CreatedAt:           timeFromUnixNano(row.CreatedAt),
		UpdatedAt:           timeFromUnixNano(row.UpdatedAt),
	}
}

func prefetchOutcomeRecordToRow(record PrefetchOutcomeRecord) prefetchOutcomeRow {
	return prefetchOutcomeRow{
		ID:                 record.ID,
		RunID:              record.RunID,
		CandidateKey:       record.CandidateKey,
		Alias:              record.Alias,
		Repository:         record.Repository,
		Reference:          record.Reference,
		SourceReference:    record.SourceReference,
		Status:             record.Status,
		Reason:             record.Reason,
		Score:              record.Score,
		ManifestDigest:     record.ManifestDigest,
		LayerCount:         record.LayerCount,
		BlobCount:          record.BlobCount,
		ChildManifestCount: record.ChildManifestCount,
		BytesWarmed:        record.BytesWarmed,
		Attempt:            record.Attempt,
		Error:              record.Error,
		SkipReason:         record.SkipReason,
		NextRetryAt:        unixNano(record.NextRetryAt),
		StartedAt:          unixNano(record.StartedAt),
		FinishedAt:         unixNano(record.FinishedAt),
		CreatedAt:          unixNano(record.CreatedAt),
	}
}

func prefetchOutcomeRowToRecord(row prefetchOutcomeRow) *PrefetchOutcomeRecord {
	return &PrefetchOutcomeRecord{
		ID:                 row.ID,
		RunID:              row.RunID,
		CandidateKey:       row.CandidateKey,
		Alias:              row.Alias,
		Repository:         row.Repository,
		Reference:          row.Reference,
		SourceReference:    row.SourceReference,
		Status:             row.Status,
		Reason:             row.Reason,
		Score:              row.Score,
		ManifestDigest:     row.ManifestDigest,
		LayerCount:         row.LayerCount,
		BlobCount:          row.BlobCount,
		ChildManifestCount: row.ChildManifestCount,
		BytesWarmed:        row.BytesWarmed,
		Attempt:            row.Attempt,
		Error:              row.Error,
		SkipReason:         row.SkipReason,
		NextRetryAt:        timeFromUnixNano(row.NextRetryAt),
		StartedAt:          timeFromUnixNano(row.StartedAt),
		FinishedAt:         timeFromUnixNano(row.FinishedAt),
		CreatedAt:          timeFromUnixNano(row.CreatedAt),
	}
}
