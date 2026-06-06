package scheduler

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func (r *Runtime) jobProviders() *collectionlist.List[ecosystem.JobProvider] {
	if r == nil {
		return collectionlist.NewList[ecosystem.JobProvider]()
	}
	return collectionlist.FilterMapList(r.runtimes, func(_ int, runtime ecosystem.Runtime) (ecosystem.JobProvider, bool) {
		provider, ok := runtime.(ecosystem.JobProvider)
		return provider, ok
	})
}
