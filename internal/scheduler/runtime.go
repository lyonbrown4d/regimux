package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	redislock "github.com/go-co-op/gocron-redis-lock/v2"
	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	goredis "github.com/redis/go-redis/v9"
	"github.com/samber/oops"
)

type Runtime struct {
	cfg      config.Config
	logger   *slog.Logger
	cleanup  *cache.CleanupService
	prefetch *prefetch.Service

	scheduler gocron.Scheduler
	redis     goredis.UniversalClient
}

func NewRuntime(
	cfg config.Config,
	logger *slog.Logger,
	cleanup *cache.CleanupService,
	metadata meta.Store,
	tags cache.TagService,
	manifests cache.ManifestService,
) *Runtime {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runtime{
		cfg:      cfg,
		logger:   logger.With("component", "scheduler"),
		cleanup:  cleanup,
		prefetch: prefetch.NewService(metadata, tags, manifests, logger),
	}
}

func (r *Runtime) Start(ctx context.Context) error {
	if r == nil || !r.cfg.Scheduler.Enabled {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	options, err := r.schedulerOptions(ctx)
	if err != nil {
		return err
	}
	scheduler, err := gocron.NewScheduler(options...)
	if err != nil {
		return oops.Wrapf(err, "create scheduler")
	}
	r.scheduler = scheduler

	if err := r.registerCleanup(ctx, scheduler); err != nil {
		return join(err, r.Stop(ctx))
	}
	if err := r.registerPrefetch(ctx, scheduler); err != nil {
		return join(err, r.Stop(ctx))
	}
	scheduler.Start()
	r.logger.Info("scheduler started", "jobs", len(scheduler.Jobs()))
	return nil
}

func (r *Runtime) Stop(ctx context.Context) error {
	if r == nil {
		return nil
	}
	var stopErr error
	if r.scheduler != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		stopErr = r.scheduler.ShutdownWithContext(ctx)
		r.scheduler = nil
	}
	if r.redis != nil {
		if err := r.redis.Close(); err != nil {
			stopErr = join(stopErr, oops.Wrapf(err, "close scheduler redis client"))
		}
		r.redis = nil
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
		_ = client.Close()
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
		_ = client.Close()
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
	return errors.Join(left, right)
}
