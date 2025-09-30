package telemetry

import (
	"sync"
	"time"
)

// SupportTicketSnapshot records ticket volume observations for a given point in time.
type SupportTicketSnapshot struct {
	Timestamp time.Time
	Count     int
	Label     string
}

// SupportTicketReport aggregates baseline and post-launch snapshots with the computed reduction.
type SupportTicketReport struct {
	Baseline  SupportTicketSnapshot
	Current   SupportTicketSnapshot
	Reduction float64
}

// SupportTicketMetrics captures baseline and post-launch ticket volumes for SC-004 validation.
type SupportTicketMetrics struct {
	mu       sync.RWMutex
	baseline SupportTicketSnapshot
	current  SupportTicketSnapshot
	clock    func() time.Time
	emitter  func(SupportTicketReport)
}

// NewSupportTicketMetrics constructs an instrument ready to record baseline and post-launch counts.
func NewSupportTicketMetrics() *SupportTicketMetrics {
	return &SupportTicketMetrics{
		mu:       sync.RWMutex{},
		baseline: SupportTicketSnapshot{Timestamp: time.Time{}, Count: 0, Label: ""},
		current:  SupportTicketSnapshot{Timestamp: time.Time{}, Count: 0, Label: ""},
		clock:    time.Now,
		emitter:  nil,
	}
}

// WithClock overrides the internal clock, primarily for testing.
func (m *SupportTicketMetrics) WithClock(clock func() time.Time) *SupportTicketMetrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	if clock == nil {
		m.clock = time.Now
	} else {
		m.clock = clock
	}
	return m
}

// SetEmitter registers a callback invoked whenever a post-launch snapshot is recorded.
func (m *SupportTicketMetrics) SetEmitter(emitter func(SupportTicketReport)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emitter = emitter
}

// RecordBaseline captures the pre-launch ticket volume snapshot.
func (m *SupportTicketMetrics) RecordBaseline(count int, label string) SupportTicketSnapshot {
	m.mu.Lock()
	snapshot := SupportTicketSnapshot{Timestamp: m.clock(), Count: count, Label: label}
	m.baseline = snapshot
	m.mu.Unlock()
	return snapshot
}

// RecordPostLaunch captures the post-launch ticket volume snapshot and optionally emits a report.
func (m *SupportTicketMetrics) RecordPostLaunch(count int, label string) SupportTicketReport {
	m.mu.Lock()
	snapshot := SupportTicketSnapshot{Timestamp: m.clock(), Count: count, Label: label}
	m.current = snapshot
	report := SupportTicketReport{
		Baseline:  m.baseline,
		Current:   snapshot,
		Reduction: reductionPercent(m.baseline.Count, snapshot.Count),
	}
	emitter := m.emitter
	m.mu.Unlock()
	if emitter != nil {
		emitter(report)
	}
	return report
}

// Snapshot returns the most recent baseline/current report without mutating state.
func (m *SupportTicketMetrics) Snapshot() SupportTicketReport {
	m.mu.RLock()
	report := SupportTicketReport{
		Baseline:  m.baseline,
		Current:   m.current,
		Reduction: reductionPercent(m.baseline.Count, m.current.Count),
	}
	m.mu.RUnlock()
	return report
}

func reductionPercent(baseline, current int) float64 {
	if baseline <= 0 {
		return 0
	}
	delta := float64(baseline-current) / float64(baseline) * 100
	return delta
}
