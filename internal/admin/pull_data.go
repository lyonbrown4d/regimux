package admin

import (
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func pullRows(records []meta.PullRecord) []PullRow {
	sorted := collectionlist.NewList(records...).Sort(comparePullRecordRecent)
	return collectionlist.MapList(sorted, func(_ int, record meta.PullRecord) PullRow {
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

type pullTimes struct {
	pull     time.Time
	upstream time.Time
}

func latestPullTimes(records []meta.PullRecord) (time.Time, time.Time) {
	times := collectionlist.ReduceList(collectionlist.NewList(records...), pullTimes{}, func(acc pullTimes, _ int, record meta.PullRecord) pullTimes {
		acc.pull = latestTime(acc.pull, record.LastPullAt)
		acc.upstream = latestTime(acc.upstream, record.LastUpstreamPullAt)
		return acc
	})
	return times.pull, times.upstream
}

func comparePullRecordRecent(left, right meta.PullRecord) int {
	return compareTimeDesc(left.LastPullAt, right.LastPullAt)
}

func mirrorCount(rows []UpstreamRow) int {
	return collectionlist.ReduceList(collectionlist.NewList(rows...), 0, func(total int, _ int, row UpstreamRow) int {
		return total + row.MirrorCount
	})
}
