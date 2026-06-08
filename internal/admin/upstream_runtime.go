package admin

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func (s *Service) configuredUpstreams() *collectionlist.List[ecosystem.Upstream] {
	if s == nil {
		return collectionlist.NewList[ecosystem.Upstream]()
	}
	return ecosystem.ConfiguredUpstreams(s.runtimes)
}
