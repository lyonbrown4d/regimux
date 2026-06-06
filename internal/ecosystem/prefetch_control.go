package ecosystem

import (
	"context"
	"time"
)

const (
	PrefetchControlActionCancel = "cancel"
	PrefetchControlActionRetry  = "retry"
)

// PrefetchControlReport summarizes an operator-requested prefetch control action.
type PrefetchControlReport struct {
	Action    string
	ActiveRun bool
	At        time.Time
}

// PrefetchController is implemented by services that can control background prefetch runs.
type PrefetchController interface {
	CancelPrefetch(context.Context) (*PrefetchControlReport, error)
	RetryPrefetch(context.Context) (*PrefetchControlReport, error)
}
