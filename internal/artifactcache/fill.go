package artifactcache

import (
	"context"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
)

type FillTracker struct {
	fills collectionmapping.ConcurrentMap[string, *Fill]
}

type Fill struct {
	done chan struct{}
	err  error
}

func NewFillTracker() *FillTracker {
	return &FillTracker{}
}

func (t *FillTracker) Begin(key Key) (*Fill, bool) {
	cacheKey := fillKey(key)
	if t == nil || cacheKey == "" {
		return nil, true
	}
	fill := &Fill{done: make(chan struct{})}
	actual, loaded := t.fills.GetOrStore(cacheKey, fill)
	return actual, !loaded
}

func (t *FillTracker) Finish(key Key, fill *Fill, err error) {
	if t == nil || fill == nil {
		return
	}
	cacheKey := fillKey(key)
	t.fills.LoadAndDelete(cacheKey)
	fill.err = err
	close(fill.done)
}

func (f *Fill) Wait(ctx context.Context) error {
	if f == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return wrapError(ctx.Err(), "wait for artifact cache fill")
	case <-f.done:
		return f.err
	}
}

func fillKey(key Key) string {
	if key.Alias == "" || key.Repository == "" || key.Reference == "" {
		return ""
	}
	return key.Alias + "\x00" + key.Repository + "\x00" + key.Reference
}
