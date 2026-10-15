package scheduler

import (
	"context"

	"github.com/samber/oops"
)

func (r *Runtime) startBackground(parent context.Context, task func(context.Context)) bool {
	r.initializeBackground()
	parent = ensureContext(parent)
	r.backgroundMu.Lock()
	if r.backgroundStopping {
		r.backgroundMu.Unlock()
		return false
	}
	r.backgroundTasks.Add(1)
	runtimeCtx := r.backgroundCtx
	r.backgroundMu.Unlock()

	taskCtx, cancel := context.WithCancel(context.WithoutCancel(parent))
	go cancelOnSignal(runtimeCtx.Done(), taskCtx.Done(), cancel)
	go func() {
		defer r.backgroundTasks.Done()
		defer cancel()
		task(taskCtx)
	}()
	return true
}

func (r *Runtime) initializeBackground() {
	r.backgroundOnce.Do(func() {
		r.backgroundCtx, r.backgroundCancel = context.WithCancel(context.Background())
		r.backgroundDone = make(chan struct{})
	})
}

func (r *Runtime) beginBackgroundStop() {
	r.initializeBackground()
	r.backgroundStopOnce.Do(func() {
		r.backgroundMu.Lock()
		r.backgroundStopping = true
		r.backgroundCancel()
		r.backgroundMu.Unlock()
		go func() {
			r.backgroundTasks.Wait()
			close(r.backgroundDone)
		}()
	})
}

func (r *Runtime) waitForBackground(ctx context.Context) error {
	r.beginBackgroundStop()
	select {
	case <-r.backgroundDone:
		return nil
	case <-ctx.Done():
		return oops.Wrapf(ctx.Err(), "wait for scheduler background tasks")
	}
}

func cancelOnSignal(stop, completed <-chan struct{}, cancel context.CancelFunc) {
	select {
	case <-stop:
		cancel()
	case <-completed:
	}
}
