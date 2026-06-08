package admin

import collectionlist "github.com/arcgolabs/collectionx/list"

func mirrorCount(rows *collectionlist.List[UpstreamRow]) int {
	if rows == nil {
		return 0
	}
	return collectionlist.ReduceList(rows, 0, func(total int, _ int, row UpstreamRow) int {
		return total + row.MirrorCount
	})
}
