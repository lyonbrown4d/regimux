package scheduler

import (
	"context"
	"log/slog"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	redislock "github.com/go-co-op/gocron-redis-lock/v2"
	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	goredis "github.com/redis/go-redis/v9"
	"github.com/samber/lo"
	"github.com/samber/oops"
	"go.uber.org/multierr"
)

type Runtime struct {
	cfg      config.Config
	logger   *slog.Logger
	runtimes *collectionlist.List[ecosystem.Runtime]
	metrics  *observability.Metrics
	metadata meta.Store

	scheduler gocron.Scheduler
	redis     goredis.UniversalClient
}

func NewRuntime(deps RuntimeDependencies) *Runtime {
	cfg := deps.Config
	logger := deps.Logger
	metrics := deps.Metrics
	if logger == nil {
		logger = slog.Default()
	}
	runtimes := collectionlist.NewList[ecosystem.Runtime]()
	if deps.Runtimes != nil {
		runtimes = collectionlist.NewList(deps.Runtimes.Values()...)
	}
	return &Runtime{
		cfg:      cfg,
		logger:   logger.With("component", "scheduler"),
		runtimes: runtimes,
		metrics:  metrics,
		metadata: deps.Metadata,
	}
}

func (r *Runtime) Start(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if !r.cfg.Scheduler.Enabled {
		r.logger.Info("scheduler disabled")
		return nil
	}
	r.logger.Info("scheduler starting",
		"cleanup_enabled", r.cfg.Scheduler.Cleanup.Enabled,
		"prefetch_enabled", r.cfg.Scheduler.Prefetch.Enabled,
		"manifest_refresh_enabled", r.cfg.Scheduler.ManifestRefresh.Enabled,
		"distributed_lock", r.cfg.Scheduler.DistributedLock,
		"ecosystems", runtimeNames(r.runtimes).Values(),
	)
	options, err := r.schedulerOptions(ctx)
	if err != nil {
		return err
	}
	scheduler, err := gocron.NewScheduler(options...)
	if err != nil {
		return oops.Wrapf(err, "create scheduler")
	}
	r.scheduler = scheduler

	if err := r.registerEcosystemJobs(ctx, scheduler); err != nil {
		return join(err, r.Stop(ctx))
	}
	if err := r.registerRefreshJob(ctx, scheduler); err != nil {
		return join(err, r.Stop(ctx))
	}
	scheduler.Start()
	if r.metrics != nil {
		r.metrics.ObserveSchedulerRuntime(ctx, len(scheduler.Jobs()))
	}
	r.logger.Info("scheduler started", "jobs", len(scheduler.Jobs()))
	return nil
}

func runtimeNames(runtimes *collectionlist.List[ecosystem.Runtime]) *collectionlist.List[string] {
	if runtimes == nil || runtimes.Len() == 0 {
		return collectionlist.NewList[string]()
	}
	names := lo.FilterMap(runtimes.Values(), func(runtime ecosystem.Runtime, _ int) (string, bool) {
		if runtime == nil {
			return "", false
		}
		return runtime.Name(), true
	})
	return collectionlist.NewList(names...)
}

func (r *Runtime) Stop(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.logger.Info("scheduler stopping")
	var stopErr error
	if r.scheduler != nil {
		stopErr = r.scheduler.ShutdownWithContext(ctx)
		r.scheduler = nil
	}
	if r.redis != nil {
		if err := r.redis.Close(); err != nil {
			stopErr = join(stopErr, oops.Wrapf(err, "close scheduler redis client"))
		}
		r.redis = nil
	}
	if stopErr == nil {
		r.logger.Info("scheduler stopped")
	}
	return stopErr
}

func (r *Runtime) schedulerOptions(ctx context.Context) ([]gocron.SchedulerOption, error) {
	if !r.shouldUseDistributedLock() {
		return nil, nil
	}
	client, locker, err := r.newRedisLocker(ctx)
	if err != nil {
		return nil, err
	}
	if locker == nil {
		return nil, nil
	}
	r.redis = client
	r.logger.Info("scheduler distributed locker enabled", "backend", r.cfg.Cache.Backend)
	return []gocron.SchedulerOption{gocron.WithDistributedLocker(locker)}, nil
}

func (r *Runtime) shouldUseDistributedLock() bool {
	if !r.cfg.Scheduler.DistributedLock {
		return false
	}
	switch r.cfg.Cache.Backend {
	case "redis", "valkey":
		return true
	default:
		return false
	}
}

func (r *Runtime) newRedisLocker(ctx context.Context) (goredis.UniversalClient, gocron.Locker, error) {
	opts := r.redisOptions()
	if len(opts.Addrs) == 0 {
		return nil, nil, nil
	}
	client := goredis.NewUniversalClient(opts)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		if closeErr := client.Close(); closeErr != nil {
			err = oops.Wrapf(multierr.Combine(err, closeErr), "close scheduler redis client after ping failure")
		}
		return nil, nil, oops.Wrapf(err, "ping scheduler redis locker")
	}

	lockOpts := []redislock.LockerOption{
		redislock.WithKeyPrefix(r.lockPrefix()),
	}
	if r.cfg.Scheduler.LockTTL > 0 {
		lockOpts = append(lockOpts, redislock.WithRedsyncOptions(redislock.WithExpiry(r.cfg.Scheduler.LockTTL)))
	}
	locker, err := redislock.NewRedisLockerWithOptions(client, lockOpts...)
	if err != nil {
		if closeErr := client.Close(); closeErr != nil {
			err = oops.Wrapf(multierr.Combine(err, closeErr), "close scheduler redis client after locker creation failure")
		}
		return nil, nil, oops.Wrapf(err, "create scheduler redis locker")
	}
	return client, locker, nil
}

func (r *Runtime) redisOptions() *goredis.UniversalOptions {
	switch r.cfg.Cache.Backend {
	case "redis":
		return &goredis.UniversalOptions{
			Addrs:    r.cfg.Cache.Redis.Addrs,
			Username: r.cfg.Cache.Redis.Username,
			Password: r.cfg.Cache.Redis.Password,
			DB:       r.cfg.Cache.Redis.DB,
		}
	case "valkey":
		return &goredis.UniversalOptions{
			Addrs:    r.cfg.Cache.Valkey.Addrs,
			Username: r.cfg.Cache.Valkey.Username,
			Password: r.cfg.Cache.Valkey.Password,
			DB:       r.cfg.Cache.Valkey.DB,
		}
	default:
		return &goredis.UniversalOptions{}
	}
}

func (r *Runtime) lockPrefix() string {
	prefix := r.cfg.Cache.Prefix
	if prefix == "" {
		return "regimux:scheduler"
	}
	return prefix + ":scheduler"
}

func join(left, right error) error {
	err := multierr.Combine(left, right)
	if err == nil {
		return nil
	}
	return oops.Wrapf(err, "join scheduler errors")
}
