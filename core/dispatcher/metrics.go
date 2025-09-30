package dispatcher

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// FanoutMetrics tracks delivery timing and efficiency for dispatcher fan-out operations.
type FanoutMetrics struct {
	totalDuration      *prometheus.HistogramVec
	perSubscriber      *prometheus.HistogramVec
	parallelEfficiency *prometheus.GaugeVec
}

// NewFanoutMetrics constructs and registers fan-out metrics with the provided registerer.
func NewFanoutMetrics(reg prometheus.Registerer) *FanoutMetrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	m := &FanoutMetrics{
		totalDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{ //nolint:exhaustruct
				Namespace: "meltica",
				Subsystem: "dispatcher",
				Name:      "fanout_total_seconds",
				Help:      "Total time to fan-out an event to all subscribers.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"subscribers"},
		),
		perSubscriber: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{ //nolint:exhaustruct
				Namespace: "meltica",
				Subsystem: "dispatcher",
				Name:      "fanout_subscriber_seconds",
				Help:      "Time spent delivering to an individual subscriber.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"subscribers"},
		),
		parallelEfficiency: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{ //nolint:exhaustruct
				Namespace: "meltica",
				Subsystem: "dispatcher",
				Name:      "fanout_parallel_efficiency",
				Help:      "Parallel efficiency ratio (0-1) comparing sequential vs actual delivery time.",
			},
			[]string{"subscribers"},
		),
	}
	reg.MustRegister(m.totalDuration, m.perSubscriber, m.parallelEfficiency)
	return m
}

// Observe records timing metrics for a completed fan-out invocation.
func (m *FanoutMetrics) Observe(subscriberCount int, perSubscriberDurations []time.Duration, total time.Duration) {
	if m == nil || subscriberCount == 0 {
		return
	}
	label := fmt.Sprintf("%d", subscriberCount)
	sequential := 0.0
	for _, dur := range perSubscriberDurations {
		if dur <= 0 {
			continue
		}
		m.perSubscriber.WithLabelValues(label).Observe(dur.Seconds())
		sequential += dur.Seconds()
	}
	if total > 0 {
		m.totalDuration.WithLabelValues(label).Observe(total.Seconds())
		denom := float64(subscriberCount) * total.Seconds()
		if denom > 0 {
			efficiency := sequential / denom
			if efficiency > 1 {
				efficiency = 1
			}
			m.parallelEfficiency.WithLabelValues(label).Set(efficiency)
		}
	}
}
