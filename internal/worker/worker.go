package worker

import (
	"context"
	"log/slog"
	"sync"

	"github.com/panjf2000/ants/v2"
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
	if ctx == nil {
		ctx = context.Background()
	}
	if len(tasks) == 0 {
		return nil
	}
	if pool == nil {
		var firstErr error
		for _, task := range tasks {
			if task == nil {
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := task(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}

	results := make(chan error, len(tasks))
	var wg sync.WaitGroup
	for _, task := range tasks {
		if task == nil {
			continue
		}
		task := task
		wg.Add(1)
		if err := pool.Submit(func() {
			defer wg.Done()
			if ctx.Err() != nil {
				results <- ctx.Err()
				return
			}
			results <- task(ctx)
		}); err != nil {
			wg.Done()
			if err := task(ctx); err != nil {
				results <- err
			}
		}
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var outErr error
	for err := range results {
		if err != nil && outErr == nil {
			outErr = err
		}
	}
	return outErr
}
