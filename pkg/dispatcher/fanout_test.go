package dispatcher

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/coachpo/meltica/pkg/events"
)

type mockRecycler struct {
	recycled []*events.Event
	mu       sync.Mutex
}

func (m *mockRecycler) RecycleEvent(ev *events.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recycled = append(m.recycled, ev)
}

func (m *mockRecycler) RecycleExecReport(er *events.ExecReport)        {}
func (m *mockRecycler) RecycleMany(evs []*events.Event)                {}
func (m *mockRecycler) EnableDebugMode()                               {}
func (m *mockRecycler) DisableDebugMode()                              {}
func (m *mockRecycler) CheckoutEvent(ev *events.Event)                 {}
func (m *mockRecycler) CheckoutExecReport(er *events.ExecReport)       {}

type mockPool struct {
	events []*events.Event
	mu     sync.Mutex
}

func (m *mockPool) Get() any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &events.Event{}
}

func (m *mockPool) Put(v any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ev, ok := v.(*events.Event); ok {
		m.events = append(m.events, ev)
	}
}

func TestNewFanout(t *testing.T) {
	rec := &mockRecycler{}
	pool := &mockPool{}
	
	fanout := NewFanout(rec, pool, nil, 4)
	
	if fanout == nil {
		t.Fatal("expected non-nil fanout")
	}
}

func TestFanoutDispatchNilEvent(t *testing.T) {
	fanout := NewFanout(nil, nil, nil, 2)
	
	ctx := context.Background()
	subscribers := []Subscriber{
		{ID: "sub1", Deliver: func(ctx context.Context, ev *events.Event) error { return nil }},
	}
	
	err := fanout.Dispatch(ctx, nil, subscribers)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFanoutDispatchNoSubscribers(t *testing.T) {
	rec := &mockRecycler{}
	fanout := NewFanout(rec, nil, nil, 2)
	
	ctx := context.Background()
	ev := &events.Event{TraceID: "test-1", Kind: events.KindMarketData}
	
	err := fanout.Dispatch(ctx, ev, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	
	// Event should be recycled
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.recycled) != 1 {
		t.Errorf("expected 1 recycled event, got %d", len(rec.recycled))
	}
}

func TestFanoutDispatchSingleSubscriber(t *testing.T) {
	rec := &mockRecycler{}
	fanout := NewFanout(rec, nil, nil, 2)
	
	ctx := context.Background()
	ev := &events.Event{TraceID: "test-1", Kind: events.KindMarketData}
	
	received := false
	subscribers := []Subscriber{
		{
			ID: "sub1",
			Deliver: func(ctx context.Context, e *events.Event) error {
				received = true
				if e.TraceID != "test-1" {
					t.Errorf("expected TraceID test-1, got %s", e.TraceID)
				}
				return nil
			},
		},
	}
	
	err := fanout.Dispatch(ctx, ev, subscribers)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	
	if !received {
		t.Error("subscriber did not receive event")
	}
}

func TestFanoutDispatchMultipleSubscribers(t *testing.T) {
	rec := &mockRecycler{}
	pool := &mockPool{}
	fanout := NewFanout(rec, pool, nil, 4)
	
	ctx := context.Background()
	ev := &events.Event{TraceID: "test-multi", Kind: events.KindMarketData}
	
	var mu sync.Mutex
	received := make(map[string]bool)
	
	subscribers := []Subscriber{
		{
			ID: "sub1",
			Deliver: func(ctx context.Context, e *events.Event) error {
				mu.Lock()
				received["sub1"] = true
				mu.Unlock()
				return nil
			},
		},
		{
			ID: "sub2",
			Deliver: func(ctx context.Context, e *events.Event) error {
				mu.Lock()
				received["sub2"] = true
				mu.Unlock()
				return nil
			},
		},
		{
			ID: "sub3",
			Deliver: func(ctx context.Context, e *events.Event) error {
				mu.Lock()
				received["sub3"] = true
				mu.Unlock()
				return nil
			},
		},
	}
	
	err := fanout.Dispatch(ctx, ev, subscribers)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 3 {
		t.Errorf("expected 3 subscribers to receive event, got %d", len(received))
	}
}

func TestFanoutDispatchSubscriberError(t *testing.T) {
	rec := &mockRecycler{}
	pool := &mockPool{}
	fanout := NewFanout(rec, pool, nil, 2)
	
	ctx := context.Background()
	ev := &events.Event{TraceID: "test-error", Kind: events.KindMarketData}
	
	expectedErr := errors.New("delivery failed")
	subscribers := []Subscriber{
		{
			ID: "sub1",
			Deliver: func(ctx context.Context, e *events.Event) error {
				return expectedErr
			},
		},
		{
			ID: "sub2",
			Deliver: func(ctx context.Context, e *events.Event) error {
				return nil
			},
		},
	}
	
	err := fanout.Dispatch(ctx, ev, subscribers)
	if err == nil {
		t.Error("expected error from failed subscriber")
	}
	
	var fanoutErr *FanoutError
	if errors.As(err, &fanoutErr) {
		if fanoutErr.TraceID != "test-error" {
			t.Errorf("expected TraceID test-error, got %s", fanoutErr.TraceID)
		}
		if fanoutErr.SubscriberCount != 2 {
			t.Errorf("expected SubscriberCount 2, got %d", fanoutErr.SubscriberCount)
		}
	} else {
		t.Error("expected FanoutError type")
	}
}

func TestFanoutDispatchSubscriberPanic(t *testing.T) {
	rec := &mockRecycler{}
	pool := &mockPool{}
	fanout := NewFanout(rec, pool, nil, 2)
	
	ctx := context.Background()
	ev := &events.Event{TraceID: "test-panic", Kind: events.KindMarketData}
	
	subscribers := []Subscriber{
		{
			ID: "panicking-sub",
			Deliver: func(ctx context.Context, e *events.Event) error {
				panic("unexpected panic")
			},
		},
		{
			ID: "normal-sub",
			Deliver: func(ctx context.Context, e *events.Event) error {
				return nil
			},
		},
	}
	
	err := fanout.Dispatch(ctx, ev, subscribers)
	if err == nil {
		t.Error("expected error from panicking subscriber")
	}
}

func TestFanoutErrorError(t *testing.T) {
	err := &FanoutError{
		Operation:         "test-fanout",
		TraceID:           "trace-123",
		EventKind:         events.KindMarketData,
		RoutingVersion:    5,
		SubscriberCount:   3,
		FailedSubscribers: []string{"sub1", "sub2"},
		Errors:            []error{errors.New("error1"), errors.New("error2")},
	}
	
	errStr := err.Error()
	if errStr == "" {
		t.Error("expected non-empty error string")
	}
	
	if err.Unwrap() == nil {
		t.Error("expected Unwrap to return errors")
	}
}

func TestFanoutErrorNil(t *testing.T) {
	var err *FanoutError
	
	errStr := err.Error()
	if errStr != "<nil>" {
		t.Errorf("expected '<nil>', got %s", errStr)
	}
	
	if err.Unwrap() != nil {
		t.Error("expected nil Unwrap for nil error")
	}
}

func TestFanoutDispatchNilSubscriberFunc(t *testing.T) {
	rec := &mockRecycler{}
	fanout := NewFanout(rec, nil, nil, 2)
	
	ctx := context.Background()
	ev := &events.Event{TraceID: "test-nil-func", Kind: events.KindMarketData}
	
	subscribers := []Subscriber{
		{ID: "sub1", Deliver: nil}, // Nil function
	}
	
	err := fanout.Dispatch(ctx, ev, subscribers)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func BenchmarkFanoutDispatch(b *testing.B) {
	rec := &mockRecycler{}
	pool := &mockPool{}
	fanout := NewFanout(rec, pool, nil, 8)
	
	ctx := context.Background()
	subscribers := []Subscriber{
		{ID: "sub1", Deliver: func(ctx context.Context, e *events.Event) error { return nil }},
		{ID: "sub2", Deliver: func(ctx context.Context, e *events.Event) error { return nil }},
		{ID: "sub3", Deliver: func(ctx context.Context, e *events.Event) error { return nil }},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev := &events.Event{TraceID: "bench", Kind: events.KindMarketData}
		_ = fanout.Dispatch(ctx, ev, subscribers)
	}
}
