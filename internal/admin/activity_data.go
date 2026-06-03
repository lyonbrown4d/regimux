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

func activityRowsFromPulls(records *collectionlist.List[meta.PullRecord], limit int) *collectionlist.List[ActivityRow] {
	if records == nil {
		return collectionlist.NewList[ActivityRow]()
	}
	return collectionlist.MapList(records.Take(limit), func(_ int, record meta.PullRecord) ActivityRow {
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
	})
}
