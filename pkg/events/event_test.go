package events

import (
	"testing"
	"time"
)

// Example unit test - same package, tests internal implementation
func TestEventReset(t *testing.T) {
	ev := &Event{
		TraceID:        "test-123",
		RoutingVersion: 5,
		Kind:           1, // arbitrary test value
		Payload:        "test data",
	}

	ev.Reset()

	if ev.TraceID != "" {
		t.Errorf("expected TraceID to be empty after Reset, got %s", ev.TraceID)
	}
	if ev.RoutingVersion != 0 {
		t.Errorf("expected RoutingVersion to be 0 after Reset, got %d", ev.RoutingVersion)
	}
	if ev.Payload != nil {
		t.Errorf("expected Payload to be nil after Reset, got %v", ev.Payload)
	}
	if ev.Kind != 0 {
		t.Errorf("expected Kind to be 0 after Reset, got %d", ev.Kind)
	}
}

func TestEventKindString(t *testing.T) {
	tests := []struct {
		kind EventKind
		want string
	}{
		{KindMarketData, "market_data"},
		{KindExecReport, "exec_report"},
		{KindControlAck, "control_ack"},
		{KindControlResult, "control_result"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("EventKind.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEventKindIsCritical(t *testing.T) {
	tests := []struct {
		name string
		kind EventKind
		want bool
	}{
		{"ExecReport is critical", KindExecReport, true},
		{"ControlAck is critical", KindControlAck, true},
		{"ControlResult is critical", KindControlResult, true},
		{"MarketData is not critical", KindMarketData, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.kind.IsCritical(); got != tt.want {
				t.Errorf("EventKind.IsCritical() = %v, want %v", got, tt.want)
			}
		})
	}
}

func BenchmarkEventReset(b *testing.B) {
	ev := &Event{
		TraceID:        "test-trace",
		RoutingVersion: 10,
		Kind:           KindMarketData,
		IngestTS:       time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev.Reset()
	}
}
