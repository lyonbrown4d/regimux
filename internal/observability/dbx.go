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
			metrics.ObserveDBOperation(ctx, driver, string(event.Operation), event.Table, event.Duration, event.Err)
		},
	}
}
