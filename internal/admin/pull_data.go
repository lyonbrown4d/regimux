package admin

import (
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func pullRows(records []meta.PullRecord) []PullRow {
	sorted := collectionlist.NewList(records...).Sort(comparePullRecordRecent)
	rows := collectionlist.NewListWithCapacity[PullRow](sorted.Len())
	sorted.Range(func(_ int, record meta.PullRecord) bool {
		rows.Add(PullRow{
			Key:                record.Key,
			Alias:              record.Alias,
			Repository:         record.Repository,
			Reference:          record.Reference,
			Count:              record.Count,
			LastPullAt:         formatTime(record.LastPullAt),
			LastUpstreamPullAt: formatTime(record.LastUpstreamPullAt),
		})
		return true
	})
	return rows.Values()
}

func limitPulls(rows []PullRow, limit int) []PullRow {
	if limit <= 0 || len(rows) <= limit {
		return rows
	}
	return rows[:limit]
}

func latestPullTimes(records []meta.PullRecord) (time.Time, time.Time) {
	var lastPullAt time.Time
	var lastUpstreamPullAt time.Time
	for i := range records {
		record := &records[i]
		lastPullAt = latestTime(lastPullAt, record.LastPullAt)
		lastUpstreamPullAt = latestTime(lastUpstreamPullAt, record.LastUpstreamPullAt)
	}
	return lastPullAt, lastUpstreamPullAt
}

func comparePullRecordRecent(left, right meta.PullRecord) int {
	return compareTimeDesc(left.LastPullAt, right.LastPullAt)
}

func mirrorCount(rows []UpstreamRow) int {
	total := 0
	for i := range rows {
		row := &rows[i]
		total += row.MirrorCount
	}
	return total
}
