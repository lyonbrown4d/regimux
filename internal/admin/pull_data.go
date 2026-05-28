package admin

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func pullRows(records []meta.PullRecord) []PullRow {
	return collectionlist.MapList(collectionlist.NewList(records...), func(_ int, record meta.PullRecord) PullRow {
		return PullRow{
			Key:                record.Key,
			Alias:              record.Alias,
			Repository:         record.Repository,
			Reference:          record.Reference,
			Count:              record.Count,
			LastPullAt:         formatTime(record.LastPullAt),
			LastUpstreamPullAt: formatTime(record.LastUpstreamPullAt),
		}
	}).Values()
}

func limitPulls(rows []PullRow, limit int) []PullRow {
	if limit <= 0 || len(rows) <= limit {
		return rows
	}
	return rows[:limit]
}

func mirrorCount(rows []UpstreamRow) int {
	return collectionlist.ReduceList(collectionlist.NewList(rows...), 0, func(total int, _ int, row UpstreamRow) int {
		return total + row.MirrorCount
	})
}
