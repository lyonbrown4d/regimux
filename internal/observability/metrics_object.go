package observability

import (
	"context"
	"sync"

	"github.com/arcgolabs/observabilityx"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type objectUploadMetrics struct {
	mu        sync.Mutex
	active    int64
	waiting   int64
	inflight  observabilityx.Gauge
	queued    observabilityx.Gauge
	admitted  observabilityx.Counter
	completed observabilityx.Counter
	waitTime  observabilityx.Histogram
	duration  observabilityx.Histogram
}

func newObjectUploadMetrics(obs observabilityx.Observability) objectUploadMetrics {
	return objectUploadMetrics{
		inflight:  obs.Gauge(gaugeSpec("object_upload_inflight", "Active S3 object uploads.")),
		queued:    obs.Gauge(gaugeSpec("object_upload_waiting", "S3 object uploads waiting for admission.")),
		admitted:  obs.Counter(counterSpec("object_upload_admissions_total", "S3 upload admission outcomes.", "result")),
		completed: obs.Counter(counterSpec("object_uploads_total", "Completed S3 uploads.", "result")),
		waitTime:  obs.Histogram(durationHistogramSpec("object_upload_wait_seconds", "S3 upload admission wait.", "result")),
		duration:  obs.Histogram(durationHistogramSpec("object_upload_duration_seconds", "S3 upload duration.", "result")),
	}
}

func (m *Metrics) ObserveS3Upload(ctx context.Context, event object.S3UploadEvent) {
	if m == nil {
		return
	}
	labels := []observabilityx.Attribute{observabilityx.String("result", resultLabel(event.Err, 0))}
	switch event.Kind {
	case "active":
		m.object.mu.Lock()
		defer m.object.mu.Unlock()
		m.object.active += event.Delta
		m.object.inflight.Set(ctx, float64(m.object.active))
	case "waiting":
		m.object.mu.Lock()
		defer m.object.mu.Unlock()
		m.object.waiting += event.Delta
		m.object.queued.Set(ctx, float64(m.object.waiting))
	case "admission":
		m.object.admitted.Add(ctx, 1, labels...)
		m.object.waitTime.Record(ctx, event.Duration.Seconds(), labels...)
	case "completed":
		m.object.completed.Add(ctx, 1, labels...)
		m.object.duration.Record(ctx, event.Duration.Seconds(), labels...)
	}
}
