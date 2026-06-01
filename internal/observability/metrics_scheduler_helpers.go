package observability

import "github.com/arcgolabs/observabilityx"

func newSchedulerJobs(obs observabilityx.Observability) observabilityx.Counter {
	return obs.Counter(counterSpec(
		"scheduler_jobs_total",
		"Total scheduler jobs.",
		"job", "alias", "result",
	))
}

func newSchedulerJobDuration(obs observabilityx.Observability) observabilityx.Histogram {
	return obs.Histogram(durationHistogramSpec(
		"scheduler_job_duration_seconds",
		"Scheduler job duration in seconds.",
		"job", "alias", "result",
	))
}

func newSchedulerJobTimestamp(obs observabilityx.Observability, name, description string) observabilityx.Gauge {
	return obs.Gauge(gaugeSpec(name, description, "job", "alias"))
}
