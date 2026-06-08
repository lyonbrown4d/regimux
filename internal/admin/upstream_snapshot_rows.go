package admin

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func upstreamMetadataMap(records *collectionlist.List[meta.Upstream]) *collectionmapping.Map[string, meta.Upstream] {
	if records == nil {
		return collectionmapping.NewMapWithCapacity[string, meta.Upstream](0)
	}
	return collectionmapping.AssociateList(
		records,
		func(_ int, row meta.Upstream) (string, meta.Upstream) {
			return row.Alias, row
		},
	)
}

func (s *Service) endpointRows(snapshot ecosystem.UpstreamSnapshot) (*collectionlist.List[EndpointRow], error) {
	return s.mapper.EndpointRows(snapshot)
}
