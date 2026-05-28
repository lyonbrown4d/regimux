package admin

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func pullRows(records *collectionlist.List[meta.PullRecord]) *collectionlist.List[PullRow] {
	if records == nil {
		return collectionlist.NewList[PullRow]()
	}
	return collectionlist.MapList(records, func(_ int, record meta.PullRecord) PullRow {
		return PullRow{
			Key:                record.Key,
			Alias:              record.Alias,
			Repository:         record.Repository,
			Reference:          record.Reference,
			Count:              record.Count,
			LastPullAt:         formatTime(record.LastPullAt),
			LastUpstreamPullAt: formatTime(record.LastUpstreamPullAt),
		}
	})
}

func mirrorCount(rows *collectionlist.List[UpstreamRow]) int {
	if rows == nil {
		return 0
	}
	return collectionlist.ReduceList(rows, 0, func(total int, _ int, row UpstreamRow) int {
		return total + row.MirrorCount
	})
}
