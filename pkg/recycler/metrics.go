package recycler

import (
	"time"

	"github.com/coachpo/meltica/pkg/events"
	"github.com/prometheus/client_golang/prometheus"
)

// RecyclerMetrics captures observability counters for recycle operations.
type RecyclerMetrics struct { //nolint:revive
	recycleTotal    *prometheus.CounterVec
	recycleDuration *prometheus.HistogramVec
	doublePutTotal  prometheus.Counter
}

// NewRecyclerMetrics constructs metrics instruments and registers them with the provided registerer.
func NewRecyclerMetrics(reg prometheus.Registerer) *RecyclerMetrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	m := &RecyclerMetrics{
		recycleTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{ //nolint:exhaustruct
				Namespace: "meltica",
				Subsystem: "recycler",
				Name:      "events_total",
				Help:      "Total number of events recycled, labeled by event kind.",
			},
			[]string{"kind"},
		),
		recycleDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{ //nolint:exhaustruct
				Namespace: "meltica",
				Subsystem: "recycler",
				Name:      "recycle_duration_seconds",
				Help:      "Time spent recycling events.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"kind"},
		),
		doublePutTotal: prometheus.NewCounter(
			prometheus.CounterOpts{ //nolint:exhaustruct
				Namespace: "meltica",
				Subsystem: "recycler",
				Name:      "double_put_total",
				Help:      "Total number of double-put violations detected in debug mode.",
			},
		),
	}
	reg.MustRegister(m.recycleTotal, m.recycleDuration, m.doublePutTotal)
	return m
}

func (m *RecyclerMetrics) observeRecycle(kind events.EventKind, started time.Time) {
	if m == nil {
		return
	}
	label := kind.String()
	m.recycleTotal.WithLabelValues(label).Inc()
	m.recycleDuration.WithLabelValues(label).Observe(time.Since(started).Seconds())
}

func (m *RecyclerMetrics) incDoublePut() {
	if m == nil {
		return
	}
	m.doublePutTotal.Inc()
}
