package unit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/observability"
)

type recordingMetrics struct {
	counters   int
	histograms int
	gauges     int
}

func (m *recordingMetrics) IncCounter(string, float64, map[string]string)       { m.counters++ }
func (m *recordingMetrics) ObserveHistogram(string, float64, map[string]string) { m.histograms++ }
func (m *recordingMetrics) SetGauge(string, float64, map[string]string)         { m.gauges++ }

func TestMetricsOverrides(t *testing.T) {
	recorder := new(recordingMetrics)
	observability.SetMetrics(recorder)

	metrics := observability.Telemetry()
	metrics.IncCounter("events", 1, nil)
	metrics.ObserveHistogram("latency", 2, nil)
	metrics.SetGauge("depth", 3, nil)

	require.Equal(t, 1, recorder.counters)
	require.Equal(t, 1, recorder.histograms)
	require.Equal(t, 1, recorder.gauges)

	observability.SetMetrics(nil)
	observability.Telemetry().IncCounter("noop", 1, nil)
	require.Equal(t, 1, recorder.counters)
}

func TestRuntimeMetricsSnapshot(t *testing.T) {
	metrics := observability.NewRuntimeMetrics()
	metrics.RecordBufferDepth("stream", 3)
	metrics.IncrementCoalescedDrops("stream")
	metrics.AddThrottledMilliseconds("stream", 5)

	snapshot := metrics.Snapshot()
	require.Equal(t, 3, snapshot.BufferDepth["stream"])
	require.Equal(t, 1, snapshot.CoalescedDrops["stream"])
	require.EqualValues(t, 5, snapshot.ThrottledMilliseconds["stream"])
}
