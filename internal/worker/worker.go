package worker

import (
	"context"
	"log/slog"

	"github.com/panjf2000/ants/v2"
	"github.com/samber/oops"
	"golang.org/x/sync/errgroup"
)

type Pools struct {
	logger       *slog.Logger
	probePool    *ants.Pool
	prefetchPool *ants.Pool
}

func NewPoolsConfig(probeConcurrency, prefetchConcurrency int, logger *slog.Logger) *Pools {
	if logger == nil {
		logger = slog.Default()
	}
	return &Pools{
		logger:       logger.With("component", "worker"),
		probePool:    newPool(probeConcurrency, logger.With("type", "probe")),
		prefetchPool: newPool(prefetchConcurrency, logger.With("type", "prefetch")),
	}
}

func (p *Pools) ProbePool() *ants.Pool {
	if p == nil {
		return nil
	}
	return p.probePool
}

func (p *Pools) PrefetchPool() *ants.Pool {
	if p == nil {
		return nil
	}
	return p.prefetchPool
}

func (p *Pools) Close() {
	if p == nil {
		return
	}
	p.logger.Info("closing worker pools")
	if p.probePool != nil {
		p.probePool.Release()
		p.probePool = nil
	}
	if p.prefetchPool != nil {
		p.prefetchPool.Release()
		p.prefetchPool = nil
	}
}

func newPool(size int, logger *slog.Logger) *ants.Pool {
	if size <= 0 {
		return nil
	}
	pool, err := ants.NewPool(size, ants.WithPanicHandler(func(recovered any) {
		if logger == nil {
			return
		}
		logger.Error("worker pool task panicked", "panic", recovered)
	}))
	if err != nil {
		if logger != nil {
			logger.Error("create worker pool failed", "size", size, "error", err)
		}
		return nil
	}
	return pool
}

func RunAll(ctx context.Context, pool *ants.Pool, tasks []func(context.Context) error) error {
	if len(tasks) == 0 {
		return nil
	}

	group, gctx := errgroup.WithContext(ctx)
	for _, task := range tasks {
		group.Go(func() error { return runOne(gctx, pool, task) })
	}
	if err := group.Wait(); err != nil {
		return oops.Wrapf(err, "run worker tasks")
	}
	return nil
}

func runOne(ctx context.Context, pool *ants.Pool, task func(context.Context) error) error {
	if task == nil {
		return nil
	}
	if pool == nil {
		return runInline(ctx, task)
	}
	return runPooled(ctx, pool, task)
}

func runInline(ctx context.Context, task func(context.Context) error) error {
	if err := task(ctx); err != nil {
		return oops.Wrapf(err, "run inline worker task")
	}
	return nil
}

func runPooled(ctx context.Context, pool *ants.Pool, task func(context.Context) error) error {
	done := make(chan error, 1)
	if err := pool.Submit(func() {
		done <- task(ctx)
	}); err != nil {
		return oops.Wrapf(err, "submit worker task")
	}

	select {
	case err := <-done:
		if err != nil {
			return oops.Wrapf(err, "run pooled worker task")
		}
		return nil
	case <-ctx.Done():
		return oops.Wrapf(ctx.Err(), "wait worker task")
	}
}
