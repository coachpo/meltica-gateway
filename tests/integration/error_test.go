package integration

import (
	"context"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/coachpo/meltica/core/dispatcher"
	"github.com/coachpo/meltica/core/events"
	"github.com/coachpo/meltica/core/recycler"
	"github.com/coachpo/meltica/internal/observability"
)

type integrationLogger struct {
	mu      sync.Mutex
	message string
	fields  []observability.Field
}

func (l *integrationLogger) Debug(string, ...observability.Field) {}

func (l *integrationLogger) Info(string, ...observability.Field) {}

func (l *integrationLogger) Error(msg string, fields ...observability.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.message = msg
	l.fields = append([]observability.Field(nil), fields...)
}

func (l *integrationLogger) snapshot() (string, []observability.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.message, append([]observability.Field(nil), l.fields...)
}

func TestFanoutErrorLoggingIncludesTraceContext(t *testing.T) {
	logger := &integrationLogger{}
	observability.SetLogger(logger)
	defer observability.SetLogger(nil)

	eventPool := &sync.Pool{New: func() any { return &events.Event{} }}
	mergedPool := &sync.Pool{New: func() any { return &events.MergedEvent{} }}
	execPool := &sync.Pool{New: func() any { return &events.ExecReport{} }}
	recyclerMetrics := recycler.NewRecyclerMetrics(prometheus.NewRegistry())
	rec := recycler.NewRecycler(eventPool, mergedPool, execPool, recyclerMetrics)
	fanout := dispatcher.NewFanout(rec, eventPool, nil, 4)

	subscribers := []dispatcher.Subscriber{
		{ID: "ok", Deliver: func(ctx context.Context, ev *events.Event) error {
			if ev != nil {
				rec.RecycleEvent(ev)
			}
			return nil
		}},
		{ID: "fail", Deliver: func(ctx context.Context, ev *events.Event) error {
			if ev != nil {
				rec.RecycleEvent(ev)
			}
			return context.Canceled
		}},
	}

	original := &events.Event{Kind: events.KindExecReport, TraceID: "trace-integration", RoutingVersion: 42}
	err := fanout.Dispatch(context.Background(), original, subscribers)
	if err == nil {
		t.Fatalf("expected aggregated error")
	}
	msg, fields := logger.snapshot()
	if msg != "operation errors" {
		t.Fatalf("unexpected log message: %q", msg)
	}
	fieldMap := make(map[string]any, len(fields))
	for _, f := range fields {
		fieldMap[f.Key] = f.Value
	}
	if got := fieldMap["trace_id"]; got != original.TraceID {
		t.Fatalf("expected trace_id %q, got %v", original.TraceID, got)
	}
	failed, ok := fieldMap["failed_subscribers"].([]string)
	if !ok {
		t.Fatalf("expected failed_subscribers field to be []string, got %T", fieldMap["failed_subscribers"])
	}
	if len(failed) == 0 || failed[0] != "fail" {
		t.Fatalf("expected failed_subscribers to include 'fail', got %v", failed)
	}
	if got := fieldMap["event_kind"]; got != original.Kind.String() {
		t.Fatalf("expected event_kind %q, got %v", original.Kind.String(), got)
	}
	if got := fieldMap["routing_version"]; got != original.RoutingVersion {
		t.Fatalf("expected routing_version %d, got %v", original.RoutingVersion, got)
	}
}
