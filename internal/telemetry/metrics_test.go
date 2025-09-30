//nolint:exhaustruct // test fixtures intentionally keep structs sparse for clarity.
package telemetry

import (
	"sync"
	"testing"
	"time"
)

func TestSupportTicketMetricsReduction(t *testing.T) {
	clockTimes := []time.Time{
		time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
	}
	idx := 0
	metrics := NewSupportTicketMetrics().WithClock(func() time.Time {
		v := clockTimes[idx]
		if idx < len(clockTimes)-1 {
			idx++
		}
		return v
	})

	baseline := metrics.RecordBaseline(100, "prelaunch")
	if baseline.Count != 100 {
		t.Fatalf("expected baseline count 100, got %d", baseline.Count)
	}
	if baseline.Timestamp != clockTimes[0] {
		t.Fatalf("unexpected baseline timestamp %s", baseline.Timestamp)
	}

	var emitted SupportTicketReport
	var mu sync.Mutex
	metrics.SetEmitter(func(report SupportTicketReport) {
		mu.Lock()
		emitted = report
		mu.Unlock()
	})

	report := metrics.RecordPostLaunch(15, "postlaunch")
	if got := report.Reduction; got < 84.9 || got > 85.1 {
		t.Fatalf("expected ~85%% reduction, got %.2f", got)
	}

	snapshot := metrics.Snapshot()
	if snapshot.Reduction != report.Reduction {
		t.Fatalf("expected snapshot reduction %.2f, got %.2f", report.Reduction, snapshot.Reduction)
	}

	mu.Lock()
	emittedCopy := emitted
	mu.Unlock()
	if emittedCopy.Reduction != report.Reduction {
		t.Fatalf("expected emitter to observe reduction %.2f, got %.2f", report.Reduction, emittedCopy.Reduction)
	}
	if emittedCopy.Current.Label != "postlaunch" {
		t.Fatalf("unexpected emitted label %s", emittedCopy.Current.Label)
	}
}

func TestSupportTicketMetricsZeroBaseline(t *testing.T) {
	metrics := NewSupportTicketMetrics()
	metrics.RecordBaseline(0, "none")
	report := metrics.RecordPostLaunch(10, "after")
	if report.Reduction != 0 {
		t.Fatalf("expected zero reduction with zero baseline, got %.2f", report.Reduction)
	}
}

func TestCollectorConfigTargetMet(t *testing.T) {
	cfg := DefaultCollectorConfig
	report := SupportTicketReport{Reduction: cfg.ReductionGoal + 0.5}
	if !cfg.TargetMet(report) {
		t.Fatal("expected target to be met")
	}
	report.Reduction = cfg.ReductionGoal - 0.1
	if cfg.TargetMet(report) {
		t.Fatal("expected target to fail")
	}
}
