package container

import (
	"context"
	"sync"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
)

func (e *RegistryEndpoint) admitManifestBlobFill(ctx context.Context, route reference.Route, manifest *cache.CachedManifest) func() {
	// Fresh manifests warm their children once. Hot hits skip both parsing and
	// the descriptor-related access writes that optional prewarming generates.
	if e == nil || e.blobs == nil || manifest == nil || manifest.Cache == cache.CacheHit {
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	if !e.prewarmGate.TryAcquire(1) {
		e.logSkippedManifestBlobFill(ctx, route, manifest.Digest, "", "prewarm admission saturated")
		return nil
	}
	return sync.OnceFunc(func() { e.prewarmGate.Release(1) })
}

type manifestBlobFillWork struct {
	endpoint  *RegistryEndpoint
	route     reference.Route
	mediaType string
	digest    string
	task      func(context.Context)
}

// A plan owns admission across descriptor parsing, canonical SQL persistence
// and worker consumption. Removing work also removes all descriptor references.
type manifestBlobFillPlan struct {
	mu         sync.Mutex
	work       *manifestBlobFillWork
	release    func()
	stopCancel func() bool
}

func newManifestBlobFillPlan(ctx context.Context, work *manifestBlobFillWork, release func()) *manifestBlobFillPlan {
	plan := &manifestBlobFillPlan{work: work, release: release}
	plan.mu.Lock()
	plan.stopCancel = context.AfterFunc(ctx, plan.discard)
	plan.mu.Unlock()
	return plan
}

func (p *manifestBlobFillPlan) discard() {
	p.mu.Lock()
	if p.work == nil {
		p.mu.Unlock()
		return
	}
	release := p.release
	p.work, p.release = nil, nil
	p.stopCancel()
	p.stopCancel = nil
	p.mu.Unlock()
	release()
}

func (p *manifestBlobFillPlan) take(ctx context.Context) (*manifestBlobFillWork, func()) {
	p.mu.Lock()
	if p.work == nil {
		p.mu.Unlock()
		return nil, nil
	}
	if ctx.Err() != nil || !p.stopCancel() {
		p.mu.Unlock()
		p.discard()
		return nil, nil
	}
	work, release := p.work, p.release
	p.work, p.release, p.stopCancel = nil, nil, nil
	p.mu.Unlock()
	return work, release
}

func (p *manifestBlobFillPlan) submit(ctx context.Context) {
	work, release := p.take(ctx)
	if work != nil {
		work.endpoint.submitManifestBlobFill(ctx, work, release)
	}
}

func (w *manifestBlobFillWork) run(ctx context.Context, release func()) {
	defer release()
	defer w.clear()
	w.task(ctx)
}

func (w *manifestBlobFillWork) clear() { *w = manifestBlobFillWork{} }
