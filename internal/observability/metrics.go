package observability

import "sync"

// Metrics provides counters, gauges, and histogram recording primitives.
type Metrics interface {
	IncCounter(name string, value float64, labels map[string]string)
	ObserveHistogram(name string, value float64, labels map[string]string)
	SetGauge(name string, value float64, labels map[string]string)
}

var defaultMetrics Metrics = noopMetrics{}

// SetMetrics overrides the global metrics implementation used by the system.
func SetMetrics(metrics Metrics) {
	if metrics == nil {
		defaultMetrics = noopMetrics{}
		return
	}
	defaultMetrics = metrics
}

// Telemetry returns the current global metrics collector.
func Telemetry() Metrics {
	return defaultMetrics
}

type noopMetrics struct{}

func (noopMetrics) IncCounter(string, float64, map[string]string)       {}
func (noopMetrics) ObserveHistogram(string, float64, map[string]string) {}
func (noopMetrics) SetGauge(string, float64, map[string]string)         {}

// DispatcherMetricsSnapshot captures dispatcher-focused runtime counters.
type DispatcherMetricsSnapshot struct {
	BufferDepth           map[string]int   `json:"buffer_depth"`
	CoalescedDrops        map[string]int   `json:"coalesced_drops"`
	ThrottledMilliseconds map[string]int64 `json:"throttled_ms"`
}

// RuntimeMetrics accumulates dispatcher metrics in-memory for periodic export.
type RuntimeMetrics struct {
	mu         sync.Mutex
	dispatcher DispatcherMetricsSnapshot
}

// NewRuntimeMetrics constructs a metrics accumulator with empty maps.
func NewRuntimeMetrics() *RuntimeMetrics {
	metrics := new(RuntimeMetrics)
	metrics.dispatcher = DispatcherMetricsSnapshot{
		BufferDepth:           make(map[string]int),
		CoalescedDrops:        make(map[string]int),
		ThrottledMilliseconds: make(map[string]int64),
	}
	return metrics
}

// RecordBufferDepth tracks the latest buffer depth for a stream key.
func (m *RuntimeMetrics) RecordBufferDepth(stream string, depth int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatcher.BufferDepth[stream] = depth
}

// IncrementCoalescedDrops increments the coalesced drop counter for a stream.
func (m *RuntimeMetrics) IncrementCoalescedDrops(stream string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatcher.CoalescedDrops[stream]++
}

// AddThrottledMilliseconds accumulates throttled time for a stream.
func (m *RuntimeMetrics) AddThrottledMilliseconds(stream string, delta int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatcher.ThrottledMilliseconds[stream] += delta
}

// Snapshot copies the current dispatcher metrics state for reporting.
func (m *RuntimeMetrics) Snapshot() DispatcherMetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := DispatcherMetricsSnapshot{
		BufferDepth:           make(map[string]int, len(m.dispatcher.BufferDepth)),
		CoalescedDrops:        make(map[string]int, len(m.dispatcher.CoalescedDrops)),
		ThrottledMilliseconds: make(map[string]int64, len(m.dispatcher.ThrottledMilliseconds)),
	}
	for k, v := range m.dispatcher.BufferDepth {
		snapshot.BufferDepth[k] = v
	}
	for k, v := range m.dispatcher.CoalescedDrops {
		snapshot.CoalescedDrops[k] = v
	}
	for k, v := range m.dispatcher.ThrottledMilliseconds {
		snapshot.ThrottledMilliseconds[k] = v
	}
	return snapshot
}
