package scheduler

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func (r *Runtime) probers() []ecosystem.Prober {
	if r == nil {
		return nil
	}
	return collectionlist.FilterMapList(collectionlist.NewList(r.runtimes...), func(_ int, runtime ecosystem.Runtime) (ecosystem.Prober, bool) {
		prober, ok := runtime.(ecosystem.Prober)
		return prober, ok
	}).Values()
}

func (r *Runtime) prefetchers() []ecosystem.Prefetcher {
	if r == nil {
		return nil
	}
	return collectionlist.FilterMapList(collectionlist.NewList(r.runtimes...), func(_ int, runtime ecosystem.Runtime) (ecosystem.Prefetcher, bool) {
		prefetcher, ok := runtime.(ecosystem.Prefetcher)
		return prefetcher, ok
	}).Values()
}

func (r *Runtime) endpointHealthFlushers() []ecosystem.EndpointHealthFlusher {
	if r == nil {
		return nil
	}
	return collectionlist.FilterMapList(collectionlist.NewList(r.runtimes...), func(_ int, runtime ecosystem.Runtime) (ecosystem.EndpointHealthFlusher, bool) {
		flusher, ok := runtime.(ecosystem.EndpointHealthFlusher)
		return flusher, ok
	}).Values()
}
