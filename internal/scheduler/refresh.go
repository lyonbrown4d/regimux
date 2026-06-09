package scheduler

import (
	"context"
	"strings"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/samber/oops"
)

const defaultRefreshWindow = 10 * time.Minute
const maxRefreshDrainInterval = time.Minute

func NewRefreshSubscriber(runtime *Runtime) events.Subscriber {
	return events.NewSubscriber[events.ArtifactPulled](func(ctx context.Context, event events.ArtifactPulled) error {
		return runtime.HandleArtifactPulled(ctx, event)
	})
}

func (r *Runtime) registerRefreshJob(ctx context.Context, scheduler gocron.Scheduler) error {
	cfg := r.refreshConfig()
	if !cfg.Enabled || cfg.Window <= 0 {
		return nil
	}

	options := []gocron.JobOption{
		gocron.WithName("regimux.refresh.drain"),
		gocron.WithTags("maintenance", "refresh"),
		gocron.WithContext(ctx),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
		gocron.WithDisabledDistributedJobLocker(!cfg.Distributed),
	}
	interval := refreshDrainInterval(cfg.Window)
	if _, err := scheduler.NewJob(
		gocron.DurationJob(interval),
		gocron.NewTask(func(ctx context.Context) error {
			return r.runRefreshDrain(ctx)
		}),
		options...,
	); err != nil {
		return oops.Wrapf(err, "register refresh drain job")
	}
	r.logger.InfoContext(ctx,
		"registered refresh drain job",
		"window", cfg.Window,
		"interval", interval,
		"distributed", cfg.Distributed,
	)
	return nil
}

func (r *Runtime) HandleArtifactPulled(ctx context.Context, event events.ArtifactPulled) error {
	if r == nil || r.metadata == nil || !refreshablePull(event) {
		return nil
	}
	if !r.cfg.Scheduler.Enabled {
		return nil
	}
	cfg := r.refreshConfig()
	if !cfg.Enabled || cfg.Window <= 0 {
		return nil
	}
	record := meta.RefreshIntentRecord{
		Ecosystem:  meta.RefreshIntentEcosystem(strings.TrimSpace(event.Ecosystem)),
		Kind:       meta.RefreshIntentKind(strings.TrimSpace(event.Kind)),
		Alias:      strings.TrimSpace(event.Alias),
		Repository: strings.TrimSpace(event.Repository),
		Reference:  strings.TrimSpace(event.Reference),
		Accept:     strings.TrimSpace(event.Accept),
	}
	intent, queued, err := r.metadata.QueueRefreshIntent(ctx, record, time.Now().UTC(), cfg.Window)
	if err != nil {
		return oops.Wrapf(err, "queue refresh intent")
	}
	if r.logger == nil {
		return nil
	}
	if queued {
		r.logger.DebugContext(ctx,
			"refresh intent queued",
			"ecosystem", intent.Ecosystem,
			"alias", intent.Alias,
			"repository", intent.Repository,
			"reference", intent.Reference,
			"kind", intent.Kind,
			"due_at", intent.DueAt,
		)
		return nil
	}
	if intent != nil && intent.Skipped > 0 {
		r.logger.DebugContext(ctx,
			"refresh intent deduplicated",
			"ecosystem", intent.Ecosystem,
			"alias", intent.Alias,
			"repository", intent.Repository,
			"reference", intent.Reference,
			"kind", intent.Kind,
			"skipped", intent.Skipped,
		)
	}
	return nil
}

func (r *Runtime) runRefreshDrain(ctx context.Context) error {
	if r == nil || r.metadata == nil {
		return nil
	}
	intents, err := r.metadata.ConsumeDueRefreshIntents(ctx, time.Now().UTC(), 100)
	if err != nil {
		return oops.Wrapf(err, "consume due refresh intents")
	}
	if len(intents) == 0 {
		return nil
	}

	startedAt := time.Now()
	var refreshErr error
	for i := range intents {
		if err := r.refreshArtifact(ctx, intents[i]); err != nil {
			refreshErr = join(refreshErr, err)
		}
	}
	r.observeJob(ctx, "refresh", "", startedAt, refreshErr)
	return refreshErr
}

func (r *Runtime) refreshArtifact(ctx context.Context, intent meta.RefreshIntentRecord) error {
	req := ecosystem.RefreshRequest{
		Ecosystem:  string(intent.Ecosystem),
		Kind:       string(intent.Kind),
		Alias:      intent.Alias,
		Repository: intent.Repository,
		Reference:  intent.Reference,
		Accept:     intent.Accept,
	}
	refresher, err := r.refresher(req.Ecosystem)
	if err != nil {
		return err
	}
	if r.logger != nil {
		r.logger.DebugContext(ctx,
			"refresh artifact started",
			"ecosystem", req.Ecosystem,
			"alias", req.Alias,
			"repository", req.Repository,
			"reference", req.Reference,
			"kind", req.Kind,
			"deduplicated", intent.Skipped,
		)
	}
	if err := refresher.Refresh(ctx, req); err != nil {
		return oops.With(
			"ecosystem", req.Ecosystem,
			"alias", req.Alias,
			"repository", req.Repository,
			"reference", req.Reference,
		).Wrapf(err, "refresh artifact")
	}
	return nil
}

func (r *Runtime) refresher(ecosystemName string) (ecosystem.Refresher, error) {
	if r == nil || r.runtimes == nil {
		return nil, oops.In("scheduler").Errorf("refresh service is not configured")
	}
	name := strings.TrimSpace(ecosystemName)
	var match ecosystem.Refresher
	r.runtimes.Range(func(_ int, runtime ecosystem.Runtime) bool {
		if runtime == nil || runtime.Name() != name {
			return true
		}
		refresher, ok := runtime.(ecosystem.Refresher)
		if ok {
			match = refresher
		}
		return false
	})
	if match == nil {
		return nil, oops.In("scheduler").With("ecosystem", name).Errorf("refresh service is not configured")
	}
	return match, nil
}

func refreshablePull(event events.ArtifactPulled) bool {
	switch strings.TrimSpace(event.Status) {
	case "hit", "stale":
		return true
	default:
		return false
	}
}

func refreshConfigWithDefaults(cfg config.SchedulerRefreshConfig) config.SchedulerRefreshConfig {
	if cfg.Window <= 0 {
		cfg.Window = defaultRefreshWindow
	}
	return cfg
}

func refreshDrainInterval(window time.Duration) time.Duration {
	if window <= 0 {
		window = defaultRefreshWindow
	}
	interval := window / 10
	if interval <= 0 {
		return time.Second
	}
	if interval > maxRefreshDrainInterval {
		return maxRefreshDrainInterval
	}
	return interval
}

func (r *Runtime) refreshConfig() config.SchedulerRefreshConfig {
	if r == nil {
		return refreshConfigWithDefaults(config.SchedulerRefreshConfig{Enabled: true, Distributed: true})
	}
	return refreshConfigWithDefaults(r.cfg.Scheduler.Refresh)
}
