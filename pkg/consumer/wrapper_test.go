package consumer

import (
	"context"
	"errors"
	"testing"

	"github.com/coachpo/meltica/pkg/events"
)

type mockRecycler struct {
	recycled []*events.Event
}

func (m *mockRecycler) RecycleEvent(ev *events.Event) {
	if m.recycled == nil {
		m.recycled = make([]*events.Event, 0)
	}
	m.recycled = append(m.recycled, ev)
}

func (m *mockRecycler) RecycleExecReport(er *events.ExecReport) {}

func (m *mockRecycler) RecycleMany(events []*events.Event) {
	for _, ev := range events {
		m.RecycleEvent(ev)
	}
}

func (m *mockRecycler) EnableDebugMode()  {}
func (m *mockRecycler) DisableDebugMode() {}

func (m *mockRecycler) CheckoutEvent(ev *events.Event)           {}
func (m *mockRecycler) CheckoutExecReport(er *events.ExecReport) {}

func TestNewWrapper(t *testing.T) {
	rec := &mockRecycler{}
	
	wrapper := NewWrapper("test-consumer", rec, nil)
	
	if wrapper == nil {
		t.Fatal("expected non-nil wrapper")
	}
	if wrapper.consumerID != "test-consumer" {
		t.Errorf("expected consumer ID test-consumer, got %s", wrapper.consumerID)
	}
}

func TestWrapperInvokeSuccess(t *testing.T) {
	rec := &mockRecycler{}
	wrapper := NewWrapper("test-consumer", rec, nil)
	
	ev := &events.Event{
		TraceID: "test-trace",
		Kind:    events.KindMarketData,
	}
	
	called := false
	lambda := func(ctx context.Context, e *events.Event) error {
		called = true
		if e.TraceID != "test-trace" {
			t.Errorf("expected trace ID test-trace, got %s", e.TraceID)
		}
		return nil
	}
	
	ctx := context.Background()
	err := wrapper.Invoke(ctx, ev, lambda)
	
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected lambda to be called")
	}
	if len(rec.recycled) != 1 {
		t.Errorf("expected 1 recycled event, got %d", len(rec.recycled))
	}
}

func TestWrapperInvokeError(t *testing.T) {
	rec := &mockRecycler{}
	wrapper := NewWrapper("test-consumer", rec, nil)
	
	ev := &events.Event{
		TraceID: "test-trace",
		Kind:    events.KindMarketData,
	}
	
	expectedErr := errors.New("lambda error")
	lambda := func(ctx context.Context, e *events.Event) error {
		return expectedErr
	}
	
	ctx := context.Background()
	err := wrapper.Invoke(ctx, ev, lambda)
	
	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if len(rec.recycled) != 1 {
		t.Error("expected event to be recycled even on error")
	}
}

func TestWrapperInvokePanic(t *testing.T) {
	rec := &mockRecycler{}
	wrapper := NewWrapper("test-consumer", rec, nil)
	
	ev := &events.Event{
		TraceID: "test-trace",
		Kind:    events.KindMarketData,
	}
	
	lambda := func(ctx context.Context, e *events.Event) error {
		panic("unexpected panic")
	}
	
	ctx := context.Background()
	err := wrapper.Invoke(ctx, ev, lambda)
	
	if err == nil {
		t.Error("expected error from panic")
	}
	if len(rec.recycled) != 1 {
		t.Error("expected event to be recycled after panic")
	}
}

func TestWrapperInvokeNilEvent(t *testing.T) {
	wrapper := NewWrapper("test-consumer", nil, nil)
	
	lambda := func(ctx context.Context, e *events.Event) error {
		t.Error("lambda should not be called with nil event")
		return nil
	}
	
	ctx := context.Background()
	err := wrapper.Invoke(ctx, nil, lambda)
	
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWrapperUpdateMinVersion(t *testing.T) {
	wrapper := NewWrapper("test-consumer", nil, nil)
	
	wrapper.UpdateMinVersion(100)
	
	ev := &events.Event{
		RoutingVersion: 50,
		Kind:           events.KindMarketData,
	}
	
	if wrapper.ShouldProcess(ev) {
		t.Error("expected event below min version to be filtered")
	}
	
	ev.RoutingVersion = 150
	if !wrapper.ShouldProcess(ev) {
		t.Error("expected event above min version to be processed")
	}
}

func TestWrapperShouldProcessCritical(t *testing.T) {
	wrapper := NewWrapper("test-consumer", nil, nil)
	wrapper.UpdateMinVersion(100)
	
	// Critical events should always be processed
	ev := &events.Event{
		RoutingVersion: 50, // Below min version
		Kind:           events.KindExecReport, // But critical
	}
	
	if !wrapper.ShouldProcess(ev) {
		t.Error("expected critical event to always be processed")
	}
}

func TestWrapperMetrics(t *testing.T) {
	wrapper := NewWrapper("test-consumer", nil, nil)
	
	// Metrics can be nil, just verify method works
	_ = wrapper.Metrics()
}
