package telemetry

import "time"

// CollectorConfig describes the sampling windows and reduction targets for telemetry reports.
type CollectorConfig struct {
	BaselineWindow time.Duration
	PostWindow     time.Duration
	ReductionGoal  float64
}

// DefaultCollectorConfig captures the production sampling cadence for SC-004 dashboards.
var DefaultCollectorConfig = CollectorConfig{
	BaselineWindow: 30 * 24 * time.Hour,
	PostWindow:     14 * 24 * time.Hour,
	ReductionGoal:  90,
}

// TargetMet reports whether the supplied ticket report satisfies the reduction goal.
func (c CollectorConfig) TargetMet(report SupportTicketReport) bool {
	return report.Reduction >= c.ReductionGoal
}
