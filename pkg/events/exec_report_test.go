package events

import (
	"testing"
)

func TestExecReportReset(t *testing.T) {
	report := &ExecReport{
		TraceID: "test-trace",
	}

	report.Reset()

	if report.TraceID != "" {
		t.Errorf("expected empty TraceID after Reset, got %s", report.TraceID)
	}
}

func TestExecReportFieldsPreservation(t *testing.T) {
	report := &ExecReport{
		TraceID: "trace-001",
	}

	// Verify field is set correctly
	if report.TraceID != "trace-001" {
		t.Error("TraceID not set correctly")
	}
}

func BenchmarkExecReportReset(b *testing.B) {
	report := &ExecReport{
		TraceID: "test-trace",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report.Reset()
	}
}
