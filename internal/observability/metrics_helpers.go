package observability

import (
	"strings"

	"github.com/arcgolabs/observabilityx"
)

func counterSpec(name, description string, labels ...string) observabilityx.CounterSpec {
	return observabilityx.NewCounterSpec(
		metricsSubsystem+"_"+name,
		observabilityx.WithDescription(description),
		observabilityx.WithLabelKeys(labels...),
	)
}

func gaugeSpec(name, description string, labels ...string) observabilityx.GaugeSpec {
	return observabilityx.NewGaugeSpec(
		metricsSubsystem+"_"+name,
		observabilityx.WithDescription(description),
		observabilityx.WithLabelKeys(labels...),
	)
}

func bytesHistogramSpec(name, description string, labels ...string) observabilityx.HistogramSpec {
	return observabilityx.NewHistogramSpec(
		metricsSubsystem+"_"+name,
		observabilityx.WithDescription(description),
		observabilityx.WithUnit("By"),
		observabilityx.WithLabelKeys(labels...),
	).WithBuckets(exponentialBuckets(512, 2, 22)...)
}

func durationHistogramSpec(name, description string, labels ...string) observabilityx.HistogramSpec {
	return observabilityx.NewHistogramSpec(
		metricsSubsystem+"_"+name,
		observabilityx.WithDescription(description),
		observabilityx.WithUnit("s"),
		observabilityx.WithLabelKeys(labels...),
	).WithBuckets(exponentialBuckets(0.001, 2, 15)...)
}

func exponentialBuckets(start, factor float64, count int) []float64 {
	if start <= 0 || factor <= 1 || count <= 0 {
		return nil
	}
	buckets := make([]float64, count)
	current := start
	for i := range count {
		buckets[i] = current
		current *= factor
	}
	return buckets
}

func resultLabel(err error, status int) string {
	if err != nil || status >= 400 {
		return "error"
	}
	return "success"
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func boolLabel(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func labelOrUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}
