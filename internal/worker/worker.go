package worker

import (
	"context"
	"log/slog"

	"github.com/panjf2000/ants/v2"
	"github.com/samber/oops"
	concpool "github.com/sourcegraph/conc/pool"
)

type Pools struct {
	logger    *slog.Logger
	ioPool    *ants.Pool
	leasePool *ants.Pool
}

func NewPoolsConfig(ioConcurrency, leaseConcurrency int, logger *slog.Logger) *Pools {
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", "worker")
	logger.Info("creating worker pools",
		"io_concurrency", ioConcurrency,
		"lease_concurrency", leaseConcurrency,
	)
	return &Pools{
		logger:    logger,
		ioPool:    newPool(ioConcurrency, logger.With("type", "io")),
		leasePool: newPool(leaseConcurrency, logger.With("type", "lease")),
	}
}

func (p *Pools) IOPool() *ants.Pool {
	if p == nil {
		return nil
	}
	return p.ioPool
}

func (p *Pools) LeasePool() *ants.Pool {
	if p == nil {
		return nil
	}
	return p.leasePool
}

func (p *Pools) Close() {
	if p == nil {
		return
	}
	p.logger.Info("closing worker pools")
	if p.ioPool != nil {
		p.ioPool.Release()
		p.ioPool = nil
	}
	if p.leasePool != nil {
		p.leasePool.Release()
		p.leasePool = nil
	}
}

func newPool(size int, logger *slog.Logger) *ants.Pool {
	if size <= 0 {
		if logger != nil {
			logger.Info("worker pool disabled", "size", size)
		}
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
	if logger != nil {
		logger.Info("worker pool created", "size", size)
	}
	return pool
}

// TaskIterable is any ordered collection of worker tasks.
type TaskIterable interface {
	Len() int
	Range(fn func(index int, task func(context.Context) error) bool)
}

func RunAll(ctx context.Context, pool *ants.Pool, tasks TaskIterable) error {
	if tasks == nil || tasks.Len() == 0 {
		return nil
	}
	if ctx == nil {
		return oops.Errorf("worker context is required")
	}

	runtime := newConcContextPool(ctx, pool).WithCancelOnError().WithFirstError()
	tasks.Range(func(_ int, task func(context.Context) error) bool {
		runtime.Go(func(taskCtx context.Context) error {
			return runOne(taskCtx, pool, task)
		})
		return true
	})
	if err := runtime.Wait(); err != nil {
		return oops.Wrapf(err, "run worker tasks")
	}
	return nil
}

func RunAllSettled(ctx context.Context, pool *ants.Pool, tasks TaskIterable) error {
	if tasks == nil || tasks.Len() == 0 {
		return nil
	}
	if ctx == nil {
		return oops.Errorf("worker context is required")
	}

	runtime := newConcContextPool(ctx, pool)
	tasks.Range(func(_ int, task func(context.Context) error) bool {
		runtime.Go(func(taskCtx context.Context) error {
			if err := runOne(taskCtx, pool, task); err != nil {
				return err
			}
			return nil
		})
		return true
	})
	if err := runtime.Wait(); err != nil {
		return oops.Wrapf(err, "run worker tasks")
	}
	return nil
}

func newConcContextPool(ctx context.Context, pool *ants.Pool) *concpool.ContextPool {
	runtimePool := concpool.New().WithContext(ctx)
	if pool != nil {
		limit := pool.Cap()
		if limit > 0 {
			runtimePool.WithMaxGoroutines(limit)
		}
	}
	return runtimePool
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
