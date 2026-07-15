package observability

import (
	"context"

	"github.com/arcgolabs/dbx"
)

func NewDBMetricsHook(metrics *Metrics, driver string) dbx.Hook {
	if metrics == nil {
		return nil
	}
	return dbx.HookFuncs{
		AfterFunc: func(ctx context.Context, event *dbx.HookEvent) {
			if event == nil {
				return
			}
			metrics.ObserveDBOperation(ctx, DBOperationMetric{
				Driver:    driver,
				Operation: string(event.Operation),
				Table:     event.Table,
				Duration:  event.Duration,
				Err:       event.Err,
			})
		},
	}
}
