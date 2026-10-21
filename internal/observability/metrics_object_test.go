package observability_test

import (
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestS3UploadMetricsRecordAdmissionAndRelease(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, nil)
	for _, event := range []object.S3UploadEvent{
		{Kind: "waiting", Delta: 1},
		{Kind: "waiting", Delta: -1},
		{Kind: "admission", Duration: time.Millisecond},
		{Kind: "active", Delta: 1},
		{Kind: "active", Delta: -1},
		{Kind: "completed", Duration: 5 * time.Millisecond},
	} {
		metrics.ObserveS3Upload(t.Context(), event)
	}
	for _, name := range []string{"service_object_upload_inflight", "service_object_upload_waiting"} {
		if gauge := lastObjectUploadMetric(t, recorder.gauges, name); gauge.value != 0 || len(gauge.attrs) != 0 {
			t.Fatalf("upload gauge was not released or has unbounded dimensions: %+v", gauge)
		}
	}
	if metric := findMetric(t, recorder.counters, "service_object_uploads_total"); metric.value != 1 {
		t.Fatalf("upload completion counter = %f, want 1", metric.value)
	}
	if metric := findMetric(t, recorder.histograms, "service_object_upload_wait_seconds"); metric.value != 0.001 {
		t.Fatalf("admission wait = %f, want seconds", metric.value)
	}
	if metric := findMetric(t, recorder.histograms, "service_object_upload_duration_seconds"); metric.value != 0.005 {
		t.Fatalf("upload duration = %f, want seconds", metric.value)
	}
}

func TestS3UploadMetricsRecordCanceledAdmission(t *testing.T) {
	recorder := &metricsRecorder{}
	metrics := observability.NewMetricsFromObservability(recorder, nil)
	metrics.ObserveS3Upload(t.Context(), object.S3UploadEvent{Kind: "waiting", Delta: 1})
	metrics.ObserveS3Upload(t.Context(), object.S3UploadEvent{Kind: "waiting", Delta: -1})
	metrics.ObserveS3Upload(t.Context(), object.S3UploadEvent{Kind: "admission", Err: context.Canceled})
	metric := findMetric(t, recorder.counters, "service_object_upload_admissions_total")
	assertMetricAttr(t, metric, "result", "error")
	if gauge := lastObjectUploadMetric(t, recorder.gauges, "service_object_upload_waiting"); gauge.value != 0 {
		t.Fatalf("canceled admission retained waiting gauge: %+v", gauge)
	}
}

func lastObjectUploadMetric(t *testing.T, records []metricRecord, name string) metricRecord {
	t.Helper()
	for index := range records {
		record := records[len(records)-1-index]
		if record.name == name {
			return record
		}
	}
	t.Fatalf("missing object upload metric %q", name)
	return metricRecord{}
}
