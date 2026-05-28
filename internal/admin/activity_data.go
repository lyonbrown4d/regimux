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
	rows := collectionlist.NewListWithCapacity[ActivityRow](min(limit, sorted.Len()))
	sorted.Range(func(index int, record meta.PullRecord) bool {
		if index >= limit {
			return false
		}
		occurredAt := latestTime(record.LastPullAt, record.LastUpstreamPullAt, record.UpdatedAt, record.CreatedAt)
		rows.Add(ActivityRow{
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
		})
		return true
	})
	return rows.Values()
}
