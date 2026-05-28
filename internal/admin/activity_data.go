package admin

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func activitySummary(snapshot metadataSnapshot) ActivitySummary {
	return ActivitySummary{
		RequestAuditAvailable: false,
		Rows:                  activityRowsFromPulls(snapshot.pulls, 50),
	}
}

func activityRowsFromPulls(records []meta.PullRecord, limit int) []ActivityRow {
	sorted := collectionlist.NewList(records...).Sort(comparePullRecordRecent)
	return collectionlist.MapList(sorted.Take(limit), func(_ int, record meta.PullRecord) ActivityRow {
		occurredAt := latestTime(record.LastPullAt, record.LastUpstreamPullAt, record.UpdatedAt, record.CreatedAt)
		return ActivityRow{
			OccurredAt: formatTime(occurredAt),
			Event:      "pull",
			Actor:      "-",
			Method:     "-",
			Path:       "-",
			Alias:      record.Alias,
			Repository: record.Repository,
			Reference:  record.Reference,
			Count:      record.Count,
			UpstreamAt: formatTime(record.LastUpstreamPullAt),
			Source:     "meta.pull_records",
			RequestID:  "-",
		}
	}).Values()
}
