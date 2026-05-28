package config_test

import (
	"context"
	"log/slog"
	"slices"
	"testing"

	"github.com/arcgolabs/configx"
	"github.com/arcgolabs/observabilityx"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestLoadWithOptionsPassesConfigxDiagnosticsOptions(t *testing.T) {
	t.Chdir(t.TempDir())

	handler := &configLogHandler{}
	logger := slog.New(handler)
	obs := &configObservabilityRecorder{}

	cfg, err := config.LoadWithOptions(
		context.Background(),
		configx.WithEnvPrefix("REGIMUX_TEST_UNUSED"),
		configx.WithLogger(logger),
		configx.WithDebug(true),
		configx.WithObservability(obs),
	)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Server.Listen == "" {
		t.Fatal("expected typed defaults to load")
	}
	if !containsString(handler.messages, "configx load started") {
		t.Fatalf("expected configx debug log, got %#v", handler.messages)
	}
	if !containsString(obs.spans, "configx.load") {
		t.Fatalf("expected configx load span, got %#v", obs.spans)
	}
	if !containsString(obs.counters, "configx_load_total") {
		t.Fatalf("expected configx load counter, got %#v", obs.counters)
	}
	if !containsString(obs.histograms, "configx_load_duration_ms") {
		t.Fatalf("expected configx load duration histogram, got %#v", obs.histograms)
	}
}

type configLogHandler struct {
	messages []string
}

func (h *configLogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *configLogHandler) Handle(_ context.Context, record slog.Record) error {
	h.messages = append(h.messages, record.Message)
	return nil
}

func (h *configLogHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h *configLogHandler) WithGroup(string) slog.Handler {
	return h
}

type configObservabilityRecorder struct {
	spans      []string
	counters   []string
	histograms []string
}

func (r *configObservabilityRecorder) Logger() *slog.Logger {
	return slog.Default()
}

func (r *configObservabilityRecorder) StartSpan(ctx context.Context, name string, attrs ...observabilityx.Attribute) (context.Context, observabilityx.Span) {
	_ = attrs
	r.spans = append(r.spans, name)
	return ctx, configSpanRecorder{}
}

func (r *configObservabilityRecorder) Counter(spec observabilityx.CounterSpec) observabilityx.Counter {
	r.counters = append(r.counters, spec.Name)
	return configCounterRecorder{}
}

func (r *configObservabilityRecorder) UpDownCounter(spec observabilityx.UpDownCounterSpec) observabilityx.UpDownCounter {
	_ = spec
	return configUpDownCounterRecorder{}
}

func (r *configObservabilityRecorder) Histogram(spec observabilityx.HistogramSpec) observabilityx.Histogram {
	r.histograms = append(r.histograms, spec.Name)
	return configHistogramRecorder{}
}

func (r *configObservabilityRecorder) Gauge(spec observabilityx.GaugeSpec) observabilityx.Gauge {
	_ = spec
	return configGaugeRecorder{}
}

type configSpanRecorder struct{}

func (configSpanRecorder) End() {}

func (configSpanRecorder) RecordError(error) {}

func (configSpanRecorder) SetAttributes(...observabilityx.Attribute) {}

type configCounterRecorder struct{}

func (configCounterRecorder) Add(context.Context, int64, ...observabilityx.Attribute) {}

type configUpDownCounterRecorder struct{}

func (configUpDownCounterRecorder) Add(context.Context, int64, ...observabilityx.Attribute) {}

type configHistogramRecorder struct{}

func (configHistogramRecorder) Record(context.Context, float64, ...observabilityx.Attribute) {}

type configGaugeRecorder struct{}

func (configGaugeRecorder) Set(context.Context, float64, ...observabilityx.Attribute) {}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
}
