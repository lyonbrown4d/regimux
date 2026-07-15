package observability

import (
	"context"
	"time"

	"github.com/arcgolabs/observabilityx"
)

type dbMetrics struct {
	operations        observabilityx.Counter
	operationDuration observabilityx.Histogram
}

func newDBMetrics(obs observabilityx.Observability) dbMetrics {
	return dbMetrics{
		operations: obs.Counter(counterSpec(
			"db_operations_total",
			"Total database operations.",
			"driver", "operation", "table", "result",
		)),
		operationDuration: obs.Histogram(durationHistogramSpec(
			"db_operation_duration_seconds",
			"Database operation duration in seconds.",
			"driver", "operation", "table", "result",
		)),
	}
}

type DBOperationMetric struct {
	Driver    string
	Operation string
	Table     string
	Duration  time.Duration
	Err       error
}

func (m *Metrics) ObserveDBOperation(ctx context.Context, metric DBOperationMetric) {
	driver := metric.Driver
	operation := metric.Operation
	table := metric.Table
	duration := metric.Duration
	err := metric.Err
	if m == nil {
		return
	}
	if duration < 0 {
		duration = 0
	}

	labels := []observabilityx.Attribute{
		observabilityx.String("driver", labelOrUnknown(driver)),
		observabilityx.String("operation", labelOrUnknown(operation)),
		observabilityx.String("table", labelOrUnknown(table)),
		observabilityx.String("result", resultLabel(err, 0)),
	}
	m.db.operations.Add(ctx, 1, labels...)
	m.db.operationDuration.Record(ctx, duration.Seconds(), labels...)
}
