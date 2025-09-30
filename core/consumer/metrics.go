// Package consumer provides consumer wrappers and related metrics.
package consumer

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ConsumerMetrics captures per-consumer invocation, panic, filter, and duration telemetry.
type ConsumerMetrics struct { //nolint:revive
	invocations *prometheus.CounterVec
	panics      *prometheus.CounterVec
	filtered    *prometheus.CounterVec
	duration    *prometheus.HistogramVec
}

// NewConsumerMetrics constructs metrics instruments registered against the supplied registerer.
func NewConsumerMetrics(reg prometheus.Registerer) *ConsumerMetrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	m := &ConsumerMetrics{
		invocations: prometheus.NewCounterVec(
			prometheus.CounterOpts{ //nolint:exhaustruct
				Namespace: "meltica",
				Subsystem: "consumer",
				Name:      "invocations_total",
				Help:      "Total number of consumer invocations.",
			},
			[]string{"consumer"},
		),
		panics: prometheus.NewCounterVec(
			prometheus.CounterOpts{ //nolint:exhaustruct
				Namespace: "meltica",
				Subsystem: "consumer",
				Name:      "panics_total",
				Help:      "Total number of consumer panics recovered.",
			},
			[]string{"consumer"},
		),
		filtered: prometheus.NewCounterVec(
			prometheus.CounterOpts{ //nolint:exhaustruct
				Namespace: "meltica",
				Subsystem: "consumer",
				Name:      "filtered_total",
				Help:      "Total number of events skipped by routing version filter.",
			},
			[]string{"consumer"},
		),
		duration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{ //nolint:exhaustruct
				Namespace: "meltica",
				Subsystem: "consumer",
				Name:      "processing_seconds",
				Help:      "Histogram of consumer processing durations.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"consumer"},
		),
	}
	reg.MustRegister(m.invocations, m.panics, m.filtered, m.duration)
	return m
}

// ObserveInvocation increments the invocation counter for the consumer.
func (m *ConsumerMetrics) ObserveInvocation(consumerID string) {
	if m == nil {
		return
	}
	m.invocations.WithLabelValues(consumerID).Inc()
}

// ObserveDuration records the processing duration for the consumer.
func (m *ConsumerMetrics) ObserveDuration(consumerID string, d time.Duration) {
	if m == nil || d < 0 {
		return
	}
	m.duration.WithLabelValues(consumerID).Observe(d.Seconds())
}

// ObservePanic increments the panic counter for the consumer.
func (m *ConsumerMetrics) ObservePanic(consumerID string) {
	if m == nil {
		return
	}
	m.panics.WithLabelValues(consumerID).Inc()
}

// ObserveFiltered increments the filtered counter for the consumer.
func (m *ConsumerMetrics) ObserveFiltered(consumerID string) {
	if m == nil {
		return
	}
	m.filtered.WithLabelValues(consumerID).Inc()
}

// InvocationsCounter exposes the invocation counter for testing and diagnostics.
func (m *ConsumerMetrics) InvocationsCounter(consumerID string) prometheus.Counter {
	return m.invocations.WithLabelValues(consumerID)
}

// PanicCounter exposes the panic counter for testing and diagnostics.
func (m *ConsumerMetrics) PanicCounter(consumerID string) prometheus.Counter {
	return m.panics.WithLabelValues(consumerID)
}

// FilteredCounter exposes the filtered counter for testing and diagnostics.
func (m *ConsumerMetrics) FilteredCounter(consumerID string) prometheus.Counter {
	return m.filtered.WithLabelValues(consumerID)
}

// DurationCollector exposes the histogram collector for testing and diagnostics.
func (m *ConsumerMetrics) DurationCollector(consumerID string) prometheus.Observer {
	return m.duration.WithLabelValues(consumerID)
}
