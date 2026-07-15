package prefetch

import "time"

type prefetchTaskOutcome struct {
	plan       candidatePlan
	result     prefetchResult
	err        error
	startedAt  time.Time
	finishedAt time.Time
}
