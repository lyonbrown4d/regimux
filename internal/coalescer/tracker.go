// Package coalescer provides small in-process fill coalescing primitives.
package coalescer

import (
	"context"
	"errors"
	"fmt"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
)

type Tracker struct {
	fills collectionmapping.ConcurrentMap[string, *Fill]
}

type Fill struct {
	done chan struct{}
	err  error
}

func NewTracker() *Tracker {
	return &Tracker{}
}

func (t *Tracker) Begin(key string) (*Fill, bool) {
	if t == nil || key == "" {
		return nil, true
	}
	fill := &Fill{done: make(chan struct{})}
	actual, loaded := t.fills.GetOrStore(key, fill)
	return actual, !loaded
}

func (t *Tracker) Current(key string) (*Fill, bool) {
	if t == nil || key == "" {
		return nil, false
	}
	return t.fills.Get(key)
}

func (t *Tracker) Finish(key string, fill *Fill, err error) {
	if t == nil || fill == nil {
		return
	}
	t.fills.LoadAndDelete(key)
	fill.err = err
	close(fill.done)
}

func (f *Fill) Wait(ctx context.Context) error {
	if f == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("wait for coalesced fill context is nil")
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for coalesced fill: %w", ctx.Err())
	case <-f.done:
		return f.err
	}
}
