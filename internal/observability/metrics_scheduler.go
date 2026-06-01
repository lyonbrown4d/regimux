package observability

import (
	"context"
	"time"

	"github.com/arcgolabs/observabilityx"
)

type schedulerMetrics struct {
	jobs             observabilityx.Counter
	jobDuration      observabilityx.Histogram
	lastJobFailure   observabilityx.Gauge
	lastJobSuccess   observabilityx.Gauge
	runtimeJobs      observabilityx.Gauge
	cleanupLastBlobs observabilityx.Gauge
	cleanupLastBytes observabilityx.Gauge
	prefetchLast     observabilityx.Gauge
	prefetchLastByte observabilityx.Gauge
}

func newSchedulerMetrics(obs observabilityx.Observability) schedulerMetrics {
	return schedulerMetrics{
		jobs:             newSchedulerJobs(obs),
		jobDuration:      newSchedulerJobDuration(obs),
		lastJobFailure:   newSchedulerJobTimestamp(obs, "scheduler_job_last_failure_timestamp_seconds", "Last failed scheduler job Unix timestamp."),
		lastJobSuccess:   newSchedulerJobTimestamp(obs, "scheduler_job_last_success_timestamp_seconds", "Last successful scheduler job Unix timestamp."),
		runtimeJobs:      obs.Gauge(gaugeSpec("scheduler_runtime_jobs", "Configured scheduler runtime jobs.")),
		cleanupLastBlobs: obs.Gauge(gaugeSpec("scheduler_cleanup_last_run_blobs", "Cleanup blobs from the last run.", "kind", "dry_run")),
		cleanupLastBytes: obs.Gauge(gaugeSpec("scheduler_cleanup_last_run_bytes", "Cleanup bytes from the last run.", "kind", "dry_run")),
		prefetchLast:     obs.Gauge(gaugeSpec("scheduler_prefetch_last_run_items", "Prefetch items from the last run.", "kind")),
		prefetchLastByte: obs.Gauge(gaugeSpec("scheduler_prefetch_last_run_bytes", "Prefetch bytes warmed by the last run.")),
	}
}

func (m *Metrics) ObserveSchedulerJob(ctx context.Context, job, alias string, duration time.Duration, err error) {
	if m == nil {
		return
	}

	result := resultLabel(err, 0)
	labels := []observabilityx.Attribute{
		observabilityx.String("job", job),
		observabilityx.String("alias", alias),
		observabilityx.String("result", result),
	}
	m.scheduler.jobs.Add(ctx, 1, labels...)
	m.scheduler.jobDuration.Record(ctx, duration.Seconds(), labels...)
	m.observeSchedulerJobTimestamp(ctx, job, alias, err)
}

func (m *Metrics) ObserveSchedulerRuntime(ctx context.Context, jobs int) {
	if m != nil {
		m.scheduler.runtimeJobs.Set(ctx, float64(jobs))
	}
}

func (m *Metrics) observeSchedulerJobTimestamp(ctx context.Context, job, alias string, err error) {
	labels := []observabilityx.Attribute{
		observabilityx.String("job", job),
		observabilityx.String("alias", alias),
	}
	if err != nil {
		m.scheduler.lastJobFailure.Set(ctx, float64(time.Now().Unix()), labels...)
		return
	}
	m.scheduler.lastJobSuccess.Set(ctx, float64(time.Now().Unix()), labels...)
}
