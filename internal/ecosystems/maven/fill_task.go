package maven

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/panjf2000/ants/v2"
)

const mavenFillTaskTimeout = 5 * time.Minute

var errMavenFillPoolClosed = errors.New("maven fill pool is closed")

type mavenFillPool struct {
	pool      *ants.Pool
	mu        sync.Mutex
	active    map[artifactcache.Key]*pendingMavenFill
	closed    bool
	closeOnce sync.Once
	closeErr  error
}

// pendingMavenFill fields are protected by owner.mu. The ready channel signals
// either publication or task failure and is closed exactly once by the worker.
type pendingMavenFill struct {
	owner    *mavenFillPool
	key      artifactcache.Key
	cancel   context.CancelFunc
	ready    chan struct{}
	refs     int
	value    *preparedMavenResponse
	err      error
	readyOK  bool
	finished bool
}

func newMavenFillPool(concurrency int) (*mavenFillPool, error) {
	if concurrency <= 0 {
		return nil, errors.New("maven fill concurrency must be positive")
	}
	workers, err := ants.NewPool(concurrency, ants.WithNonblocking(true))
	if err != nil {
		return nil, wrapError(err, "create maven fill worker pool")
	}
	return &mavenFillPool{pool: workers, active: make(map[artifactcache.Key]*pendingMavenFill)}, nil
}

func (p *mavenFillPool) join(key artifactcache.Key) *pendingMavenFill {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	task := p.active[key]
	if task != nil {
		task.refs++
	}
	return task
}

func (p *mavenFillPool) acquire(ctx context.Context, key artifactcache.Key, work func(context.Context, *pendingMavenFill) error) (*pendingMavenFill, bool, error) {
	if ctx == nil {
		return nil, false, errors.New("maven fill acquire context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, wrapError(err, "acquire maven fill")
	}
	if work == nil {
		return nil, false, errors.New("maven fill work is nil")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, false, errMavenFillPoolClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, false, wrapError(err, "acquire maven fill")
	}
	if existing := p.active[key]; existing != nil {
		existing.refs++
		return existing, false, nil
	}

	taskCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mavenFillTaskTimeout)
	task := &pendingMavenFill{
		owner:  p,
		key:    key,
		cancel: cancel,
		ready:  make(chan struct{}),
		refs:   1,
	}
	p.active[key] = task
	if err := p.pool.Submit(func() { p.run(taskCtx, task, work) }); err != nil {
		delete(p.active, key)
		cancel()
		return nil, false, wrapError(err, "submit maven fill")
	}
	return task, true, nil
}

func (p *mavenFillPool) run(ctx context.Context, task *pendingMavenFill, work func(context.Context, *pendingMavenFill) error) {
	var workErr error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				workErr = fmt.Errorf("maven fill task panic: %v", recovered)
			}
		}()
		workErr = work(ctx, task)
	}()
	task.cancel()
	p.finish(task, workErr)
}

func (p *mavenFillPool) finish(task *pendingMavenFill, workErr error) {
	var cleanup *preparedMavenResponse
	p.mu.Lock()
	task.finished = true
	if !task.readyOK {
		if workErr == nil {
			workErr = errors.New("maven fill finished without publishing a response")
		}
		task.err = workErr
		task.readyOK = true
		close(task.ready)
	}
	if p.active[task.key] == task {
		delete(p.active, task.key)
	}
	cleanup = p.cleanupLocked(task)
	p.mu.Unlock()
	removePreparedMavenResponse(cleanup)
}

// publish transfers prepared-response cleanup ownership to the task. A false
// result means the caller still owns cleanup responsibility.
func (task *pendingMavenFill) publish(prepared *preparedMavenResponse) bool {
	if task == nil || task.owner == nil || prepared == nil {
		return false
	}
	owner := task.owner
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if task.readyOK || task.finished {
		return false
	}
	task.value = prepared
	task.readyOK = true
	close(task.ready)
	return true
}

func (task *pendingMavenFill) wait(ctx context.Context) (*preparedMavenResponse, error) {
	if task == nil {
		return nil, errors.New("maven fill task is nil")
	}
	if ctx == nil {
		return nil, errors.New("maven fill wait context is nil")
	}
	if waitErr := ctx.Err(); waitErr != nil {
		return nil, wrapError(waitErr, "wait for maven fill")
	}
	select {
	case <-ctx.Done():
		return nil, wrapError(ctx.Err(), "wait for maven fill")
	case <-task.ready:
	}
	if waitErr := ctx.Err(); waitErr != nil {
		return nil, wrapError(waitErr, "wait for maven fill")
	}
	task.owner.mu.Lock()
	prepared, err := task.value, task.err
	task.owner.mu.Unlock()
	if waitErr := ctx.Err(); waitErr != nil {
		return nil, wrapError(waitErr, "wait for maven fill")
	}
	return prepared, err
}

func (task *pendingMavenFill) release() {
	if task == nil || task.owner == nil {
		return
	}
	owner := task.owner
	var cleanup *preparedMavenResponse
	var cancel context.CancelFunc
	owner.mu.Lock()
	if task.refs > 0 {
		task.refs--
	}
	if task.refs == 0 && !task.readyOK {
		cancel = task.cancel
	}
	cleanup = owner.cleanupLocked(task)
	owner.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	removePreparedMavenResponse(cleanup)
}

func (p *mavenFillPool) cleanupLocked(task *pendingMavenFill) *preparedMavenResponse {
	if !task.finished || task.refs != 0 {
		return nil
	}
	prepared := task.value
	task.value = nil
	return prepared
}

func removePreparedMavenResponse(prepared *preparedMavenResponse) {
	if prepared != nil && prepared.replay != nil {
		prepared.replay.Remove()
	}
}

func (p *mavenFillPool) close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		cancels := make([]context.CancelFunc, 0, len(p.active))
		for _, task := range p.active {
			cancels = append(cancels, task.cancel)
		}
		p.mu.Unlock()
		for _, cancel := range cancels {
			cancel()
		}
		p.closeErr = p.pool.ReleaseTimeout(5 * time.Second)
	})
	return p.closeErr
}
