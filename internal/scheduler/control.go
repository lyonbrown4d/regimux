package scheduler

import (
	"context"
	"fmt"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/oops"
)

func (r *Runtime) TriggerCleanup(ctx context.Context) error {
	ctx = ensureContext(ctx)
	if r == nil {
		return oops.In("scheduler").Errorf("scheduler is not configured")
	}
	return r.runAsync(ctx, "regimux.cache.cleanup.manual", []string{"maintenance", "cleanup", "manual"}, func(ctx context.Context) error {
		return r.runCleanup(ctx)
	})
}

func (r *Runtime) TriggerProbe(ctx context.Context, ecosystemName, alias string) error {
	ctx = ensureContext(ctx)
	if r == nil {
		return oops.In("scheduler").Errorf("scheduler is not configured")
	}

	prober, target, err := r.findProbeTarget(ecosystemName, alias)
	if err != nil {
		return err
	}
	jobName := fmt.Sprintf("regimux.%s.probe.%s.manual", target.Ecosystem, target.Alias)
	return r.runAsync(ctx, jobName, []string{"maintenance", "probe", target.Ecosystem, target.Alias, "manual"}, func(ctx context.Context) error {
		return r.runProbe(ctx, prober, target)
	})
}

func (r *Runtime) runAsync(ctx context.Context, jobName string, tags []string, fn func(context.Context) error) error {
	ctx = ensureContext(ctx)
	if r == nil {
		return oops.In("scheduler").Errorf("scheduler is not configured")
	}
	if fn == nil {
		return oops.In("scheduler").Errorf("scheduled task is not configured")
	}
	if r.scheduler == nil {
		go func() {
			taskCtx := context.WithoutCancel(ctx)
			if err := fn(taskCtx); err != nil && r.logger != nil {
				r.logger.WarnContext(taskCtx, "manual scheduler task failed", "job", jobName, "error", err)
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
		r.logger.InfoContext(ctx, "manual scheduler task submitted", "job", jobName)
	}
	return nil
}

func (r *Runtime) findProbeTarget(ecosystemName, alias string) (ecosystem.Prober, ecosystem.ProbeTarget, error) {
	if r == nil {
		return nil, ecosystem.ProbeTarget{}, oops.In("scheduler").Errorf("scheduler is not configured")
	}
	normalized := normalizeStringPair(ecosystemName, alias)
	if normalized.ecosystem == "" {
		return nil, ecosystem.ProbeTarget{}, oops.In("scheduler").Errorf("ecosystem is required")
	}
	if normalized.alias == "" {
		return nil, ecosystem.ProbeTarget{}, oops.In("scheduler").Errorf("alias is required")
	}

	runtime, err := r.runtimeByName(normalized.ecosystem)
	if err != nil {
		return nil, ecosystem.ProbeTarget{}, err
	}
	prober, ok := runtime.(ecosystem.Prober)
	if !ok {
		return nil, ecosystem.ProbeTarget{}, oops.In("scheduler").With("ecosystem", normalized.ecosystem).Errorf("ecosystem prober is not configured")
	}

	if target, found := r.findProbeTargetFromUpstreams(normalized.alias, runtime); found {
		return prober, target, nil
	}
	if target, found := r.findProbeTargetFromProbes(normalized.alias, prober.ProbeTargets()); found {
		return prober, target, nil
	}

	return nil, ecosystem.ProbeTarget{}, oops.In("scheduler").With("ecosystem", normalized.ecosystem, "alias", normalized.alias).Errorf("probe target not found")
}

func (r *Runtime) runtimeByName(name string) (ecosystem.Runtime, error) {
	for _, runtime := range r.runtimes {
		if runtime != nil && strings.EqualFold(runtime.Name(), name) {
			return runtime, nil
		}
	}
	return nil, oops.In("scheduler").With("ecosystem", name).Errorf("ecosystem prober is not configured")
}

func (r *Runtime) findProbeTargetFromUpstreams(alias string, rt ecosystem.Runtime) (ecosystem.ProbeTarget, bool) {
	upstreamProvider, ok := rt.(ecosystem.UpstreamProvider)
	if !ok {
		return ecosystem.ProbeTarget{}, false
	}
	upstreams := upstreamProvider.Upstreams()
	target := probeTargetFromUpstreams(alias, upstreams)
	return target, target.Alias != ""
}

func probeTargetFromUpstreams(alias string, upstreams *collectionlist.List[ecosystem.Upstream]) ecosystem.ProbeTarget {
	if upstreams == nil {
		return ecosystem.ProbeTarget{}
	}
	var matched ecosystem.ProbeTarget
	upstreams.Range(func(_ int, upstream ecosystem.Upstream) bool {
		if strings.EqualFold(strings.TrimSpace(upstream.Alias), alias) {
			matched = ecosystem.ProbeTarget(upstream)
			return false
		}
		return true
	})
	return matched
}

func (r *Runtime) findProbeTargetFromProbes(alias string, probes *collectionlist.List[ecosystem.ProbeTarget]) (ecosystem.ProbeTarget, bool) {
	if probes == nil {
		return ecosystem.ProbeTarget{}, false
	}
	var matched ecosystem.ProbeTarget
	var found bool
	probes.Range(func(_ int, target ecosystem.ProbeTarget) bool {
		if strings.EqualFold(strings.TrimSpace(target.Alias), alias) {
			matched = target
			found = true
			return false
		}
		return true
	})
	return matched, found
}

type normalizedProbeLookup struct {
	ecosystem string
	alias     string
}

func normalizeStringPair(ecosystemName, alias string) normalizedProbeLookup {
	return normalizedProbeLookup{
		ecosystem: strings.TrimSpace(ecosystemName),
		alias:     strings.TrimSpace(alias),
	}
}

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
