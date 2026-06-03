package scheduler

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/oops"
)

func (r *Runtime) TriggerCleanup(context.Context) error {
	if r == nil {
		return oops.In("scheduler").Errorf("scheduler is not configured")
	}
	return r.runAsync(context.Background(), "regimux.cache.cleanup.manual", []string{"maintenance", "cleanup", "manual"}, func(ctx context.Context) error {
		return r.runCleanup(ctx)
	})
}

func (r *Runtime) TriggerProbe(_ context.Context, ecosystemName, alias string) error {
	if r == nil {
		return oops.In("scheduler").Errorf("scheduler is not configured")
	}
	ecosystemName = strings.TrimSpace(ecosystemName)
	alias = strings.TrimSpace(alias)
	if ecosystemName == "" {
		return oops.In("scheduler").Errorf("ecosystem is required")
	}
	if alias == "" {
		return oops.In("scheduler").Errorf("alias is required")
	}

	prober, target, err := r.findProbeTarget(ecosystemName, alias)
	if err != nil {
		return err
	}
	jobName := fmt.Sprintf("regimux.%s.probe.%s.manual", target.Ecosystem, target.Alias)
	return r.runAsync(context.Background(), jobName, []string{"maintenance", "probe", target.Ecosystem, target.Alias, "manual"}, func(ctx context.Context) error {
		return r.runProbe(ctx, prober, target)
	})
}

func (r *Runtime) runAsync(ctx context.Context, jobName string, tags []string, fn func(context.Context) error) error {
	if r == nil {
		return oops.In("scheduler").Errorf("scheduler is not configured")
	}
	if fn == nil {
		return oops.In("scheduler").Errorf("scheduled task is not configured")
	}
	if r.scheduler == nil {
		go func() {
			if err := fn(context.Background()); err != nil && r.logger != nil {
				r.logger.WarnContext(context.Background(), "manual scheduler task failed", "job", jobName, "error", err)
			}
		}()
		return nil
	}
	task := gocron.NewTask(fn)
	_, err := r.scheduler.NewJob(
		gocron.OneTimeJob(gocron.OneTimeJobStartImmediately()),
		task,
		gocron.WithName(jobName),
		gocron.WithTags(tags...),
		gocron.WithContext(ctx),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
	)
	if err != nil {
		return oops.Wrapf(err, "submit manual scheduler task")
	}
	if r.logger != nil {
		r.logger.InfoContext(context.Background(), "manual scheduler task submitted", "job", jobName)
	}
	return nil
}

func (r *Runtime) findProbeTarget(ecosystemName, alias string) (ecosystem.Prober, ecosystem.ProbeTarget, error) {
	if r == nil {
		return nil, ecosystem.ProbeTarget{}, oops.In("scheduler").Errorf("scheduler is not configured")
	}
	ecosystemName = strings.TrimSpace(ecosystemName)
	alias = strings.TrimSpace(alias)
	if ecosystemName == "" {
		return nil, ecosystem.ProbeTarget{}, oops.In("scheduler").Errorf("ecosystem is required")
	}
	if alias == "" {
		return nil, ecosystem.ProbeTarget{}, oops.In("scheduler").Errorf("alias is required")
	}

	for _, runtime := range r.runtimes {
		if runtime == nil || !strings.EqualFold(runtime.Name(), ecosystemName) {
			continue
		}
		prober, ok := runtime.(ecosystem.Prober)
		if !ok {
			continue
		}
		if upstreamProvider, ok := runtime.(ecosystem.UpstreamProvider); ok {
			upstreams := upstreamProvider.Upstreams()
			var (
				matched ecosystem.ProbeTarget
				found   bool
			)
			upstreams.Range(func(_ int, upstream ecosystem.Upstream) bool {
				if strings.EqualFold(strings.TrimSpace(upstream.Alias), alias) {
					matched = ecosystem.ProbeTarget{
						Ecosystem: upstream.Ecosystem,
						Alias:     upstream.Alias,
						Config:    upstream.Config,
					}
					found = true
					return false
				}
				return true
			})
			if found {
				return prober, matched, nil
			}
		}
		targets := prober.ProbeTargets()
		if targets == nil {
			continue
		}
		found := false
		var matched ecosystem.ProbeTarget
		targets.Range(func(_ int, target ecosystem.ProbeTarget) bool {
			if strings.EqualFold(strings.TrimSpace(target.Alias), alias) {
				matched = target
				found = true
				return false
			}
			return true
		})
		if found {
			return prober, matched, nil
		}
		return nil, ecosystem.ProbeTarget{}, oops.In("scheduler").With("ecosystem", ecosystemName, "alias", alias).Errorf("probe target not found")
	}
	return nil, ecosystem.ProbeTarget{}, oops.In("scheduler").With("ecosystem", ecosystemName, "alias", alias).Errorf("ecosystem prober is not configured")
}
