package scheduler

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func (r *Runtime) probers() *collectionlist.List[ecosystem.Prober] {
	if r == nil {
		return collectionlist.NewList[ecosystem.Prober]()
	}
	return collectionlist.FilterMapList(r.runtimes, func(_ int, runtime ecosystem.Runtime) (ecosystem.Prober, bool) {
		prober, ok := runtime.(ecosystem.Prober)
		return prober, ok
	})
}

func (r *Runtime) prefetchers() *collectionlist.List[ecosystem.Prefetcher] {
	if r == nil {
		return collectionlist.NewList[ecosystem.Prefetcher]()
	}
	return collectionlist.FilterMapList(r.runtimes, func(_ int, runtime ecosystem.Runtime) (ecosystem.Prefetcher, bool) {
		prefetcher, ok := runtime.(ecosystem.Prefetcher)
		return prefetcher, ok
	})
}

func (r *Runtime) endpointHealthFlushers() *collectionlist.List[ecosystem.EndpointHealthFlusher] {
	if r == nil {
		return collectionlist.NewList[ecosystem.EndpointHealthFlusher]()
	}
	return collectionlist.FilterMapList(r.runtimes, func(_ int, runtime ecosystem.Runtime) (ecosystem.EndpointHealthFlusher, bool) {
		flusher, ok := runtime.(ecosystem.EndpointHealthFlusher)
		return flusher, ok
	})
}
