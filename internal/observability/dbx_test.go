package observability_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/arcgolabs/dbx"
	"github.com/arcgolabs/observabilityx"
	"github.com/lyonbrown4d/regimux/internal/observability"
)

func TestNewDBMetricsHookRecordsOperationMetrics(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, nil)
	hook := observability.NewDBMetricsHook(metrics, "sqlite")
	if hook == nil {
		t.Fatal("expected db metrics hook")
	}

	hook.After(context.Background(), &dbx.HookEvent{
		Operation: dbx.OperationExec,
		Table:     "blobs",
		Duration:  2 * time.Millisecond,
		Err:       errors.New("write failed"),
	})

	counter := findMetric(t, recorder.counters, "service_db_operations_total")
	assertMetricAttr(t, counter, "driver", "sqlite")
	assertMetricAttr(t, counter, "operation", "exec")
	assertMetricAttr(t, counter, "table", "blobs")
	assertMetricAttr(t, counter, "result", "error")

	histogram := findMetric(t, recorder.histograms, "service_db_operation_duration_seconds")
	if histogram.value <= 0 {
		t.Fatalf("expected positive duration metric, got %f", histogram.value)
	}
	assertMetricAttr(t, histogram, "result", "error")
}

type metricRecord struct {
	name  string
	value float64
	attrs map[string]any
}

type metricsRecorder struct {
	counters   []metricRecord
	histograms []metricRecord
}

func (r *metricsRecorder) Logger() *slog.Logger {
	return slog.Default()
}

func (r *metricsRecorder) StartSpan(ctx context.Context, name string, attrs ...observabilityx.Attribute) (context.Context, observabilityx.Span) {
	_ = name
	_ = attrs
	return ctx, metricsSpan{}
}

func (r *metricsRecorder) Counter(spec observabilityx.CounterSpec) observabilityx.Counter {
	return metricCounter{recorder: r, name: spec.Name}
}

func (r *metricsRecorder) UpDownCounter(spec observabilityx.UpDownCounterSpec) observabilityx.UpDownCounter {
	_ = spec
	return metricUpDownCounter{}
}

func (r *metricsRecorder) Histogram(spec observabilityx.HistogramSpec) observabilityx.Histogram {
	return metricHistogram{recorder: r, name: spec.Name}
}

func (r *metricsRecorder) Gauge(spec observabilityx.GaugeSpec) observabilityx.Gauge {
	_ = spec
	return metricGauge{}
}

type metricCounter struct {
	recorder *metricsRecorder
	name     string
}

func (c metricCounter) Add(_ context.Context, value int64, attrs ...observabilityx.Attribute) {
	c.recorder.counters = append(c.recorder.counters, metricRecord{
		name:  c.name,
		value: float64(value),
		attrs: metricAttrs(attrs),
	})
}

type metricHistogram struct {
	recorder *metricsRecorder
	name     string
}

func (h metricHistogram) Record(_ context.Context, value float64, attrs ...observabilityx.Attribute) {
	h.recorder.histograms = append(h.recorder.histograms, metricRecord{
		name:  h.name,
		value: value,
		attrs: metricAttrs(attrs),
	})
}

type metricUpDownCounter struct{}

func (metricUpDownCounter) Add(context.Context, int64, ...observabilityx.Attribute) {}

type metricGauge struct{}

func (metricGauge) Set(context.Context, float64, ...observabilityx.Attribute) {}

type metricsSpan struct{}

func (metricsSpan) End() {}

func (metricsSpan) RecordError(error) {}

func (metricsSpan) SetAttributes(...observabilityx.Attribute) {}

func metricAttrs(attrs []observabilityx.Attribute) map[string]any {
	out := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		out[attr.Key] = attr.Value
	}
	return out
}

func findMetric(t *testing.T, records []metricRecord, name string) metricRecord {
	t.Helper()
	for _, record := range records {
		if record.name == name {
			return record
		}
	}
	t.Fatalf("missing metric %q in %#v", name, records)
	return metricRecord{}
}

func assertMetricAttr(t *testing.T, record metricRecord, key string, want any) {
	t.Helper()
	if got := record.attrs[key]; got != want {
		t.Fatalf("unexpected %s attr: got=%v want=%v record=%#v", key, got, want, record)
	}
}
