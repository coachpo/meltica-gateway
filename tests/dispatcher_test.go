package tests

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/coachpo/meltica/core/dispatcher"
	"github.com/coachpo/meltica/core/events"
	"github.com/coachpo/meltica/internal/observability"
)

func TestFanoutDeliversWithinLatency(t *testing.T) {
	recycler, pool := newTrackingRecycler(t)
	metrics := dispatcher.NewFanoutMetrics(prometheus.NewRegistry())
	fanout := dispatcher.NewFanout(recycler, pool, metrics, 10)

	subscribers := make([]dispatcher.Subscriber, 10)
	for i := range subscribers {
		subscribers[i] = dispatcher.Subscriber{
			ID: fmt.Sprintf("sub-%d", i),
			Deliver: func(ctx context.Context, ev *events.Event) error {
				defer recycler.RecycleEvent(ev)
				time.Sleep(5 * time.Millisecond)
				return nil
			},
		}
	}

	original := &events.Event{Kind: events.KindMarketData}
	start := time.Now()
	if err := fanout.Dispatch(context.Background(), original, subscribers); err != nil {
		t.Fatalf("fanout dispatch err: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 40*time.Millisecond {
		t.Fatalf("fan-out took too long: %s", elapsed)
	}
	if !recycler.WasRecycled(original) {
		t.Fatalf("original event was not recycled")
	}
	expected := int64(len(subscribers) + 1)
	if recycler.recycled.Load() < expected {
		t.Fatalf("expected at least %d recycled events, got %d", expected, recycler.recycled.Load())
	}
}

func TestFanoutRaceSafe(t *testing.T) {
	t.Parallel()
	recycler, pool := newTrackingRecycler(t)
	fanout := dispatcher.NewFanout(recycler, pool, nil, 8)

	subscribers := []dispatcher.Subscriber{
		{ID: "a", Deliver: func(ctx context.Context, ev *events.Event) error {
			defer recycler.RecycleEvent(ev)
			return nil
		}},
		{ID: "b", Deliver: func(ctx context.Context, ev *events.Event) error {
			defer recycler.RecycleEvent(ev)
			return nil
		}},
	}

	const iterations = 64
	var wg sync.WaitGroup
	wg.Add(iterations)
	for i := 0; i < iterations; i++ {
		go func() {
			defer wg.Done()
			original := &events.Event{Kind: events.KindMarketData}
			if err := fanout.Dispatch(context.Background(), original, subscribers); err != nil {
				t.Errorf("fanout dispatch err: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestFanoutPoolUtilizationUnderLoad(t *testing.T) {
	recycler, pool := newTrackingRecycler(t)
	metrics := dispatcher.NewFanoutMetrics(prometheus.NewRegistry())
	fanout := dispatcher.NewFanout(recycler, pool, metrics, 6)

	// Pre-populate pool with a small set of events to encourage reuse.
	for i := 0; i < 32; i++ {
		pool.Put(&events.Event{})
	}

	var (
		mu   sync.Mutex
		seen = make(map[uintptr]struct{})
	)
	subscribers := make([]dispatcher.Subscriber, 5)
	for i := range subscribers {
		subscribers[i] = dispatcher.Subscriber{
			ID: fmt.Sprintf("sub-%d", i),
			Deliver: func(ctx context.Context, ev *events.Event) error {
				ptr := uintptr(unsafe.Pointer(ev))
				mu.Lock()
				seen[ptr] = struct{}{}
				mu.Unlock()
				recycler.RecycleEvent(ev)
				return nil
			},
		}
	}

	const eventsCount = 100
	for i := 0; i < eventsCount; i++ {
		original := &events.Event{Kind: events.KindMarketData}
		if err := fanout.Dispatch(context.Background(), original, subscribers); err != nil {
			t.Fatalf("fanout dispatch err: %v", err)
		}
	}

	unique := len(seen)
	total := eventsCount * len(subscribers)
	utilization := float64(unique) / float64(total)
	if utilization >= 0.8 {
		t.Fatalf("expected <80%% unique allocations, got %.2f (unique=%d total=%d)", utilization, unique, total)
	}
}

func TestFanoutRecyclesOriginalAfterDispatch(t *testing.T) {
	recycler, pool := newTrackingRecycler(t)
	fanout := dispatcher.NewFanout(recycler, pool, nil, 4)

	subscribers := []dispatcher.Subscriber{
		{ID: "sub", Deliver: func(ctx context.Context, ev *events.Event) error {
			defer recycler.RecycleEvent(ev)
			return nil
		}},
	}

	original := &events.Event{Kind: events.KindExecReport}
	if err := fanout.Dispatch(context.Background(), original, append(subscribers, subscribers...)); err != nil {
		t.Fatalf("fanout dispatch err: %v", err)
	}
	if !recycler.WasRecycled(original) {
		t.Fatalf("expected original event to be recycled")
	}
}

type countingPool struct {
	inner *sync.Pool
	gets  atomic.Int64
}

func (c *countingPool) Get() any {
	c.gets.Add(1)
	return c.inner.Get()
}

func (c *countingPool) Put(v any) {
	c.inner.Put(v)
}

func TestFanoutSingleSubscriberAvoidsDuplicate(t *testing.T) {
	recycler, _ := newTrackingRecycler(t)
	inner := &sync.Pool{New: func() any { return &events.Event{} }}
	pool := &countingPool{inner: inner}
	fanout := dispatcher.NewFanout(recycler, pool, nil, 4)

	original := &events.Event{Kind: events.KindMarketData}
	subscriber := dispatcher.Subscriber{
		ID: "single",
		Deliver: func(ctx context.Context, ev *events.Event) error {
			recycler.RecycleEvent(ev)
			return nil
		},
	}

	if err := fanout.Dispatch(context.Background(), original, []dispatcher.Subscriber{subscriber}); err != nil {
		t.Fatalf("fanout dispatch err: %v", err)
	}
	if pool.gets.Load() != 0 {
		t.Fatalf("expected no duplicate allocations, observed %d", pool.gets.Load())
	}
	if !recycler.WasRecycled(original) {
		t.Fatalf("expected original to be recycled by subscriber")
	}
}

func TestFanoutAggregatesSubscriberErrors(t *testing.T) {
	recycler, pool := newTrackingRecycler(t)
	fanout := dispatcher.NewFanout(recycler, pool, nil, 8)
	logger := newCaptureLogger()
	observability.SetLogger(logger)
	defer observability.SetLogger(nil)

	errBoom := errors.New("subscriber failure")
	subscribers := []dispatcher.Subscriber{
		{ID: "ok", Deliver: func(ctx context.Context, ev *events.Event) error {
			defer recycler.RecycleEvent(ev)
			return nil
		}},
		{ID: "fail", Deliver: func(ctx context.Context, ev *events.Event) error {
			defer recycler.RecycleEvent(ev)
			return errBoom
		}},
	}

	original := &events.Event{Kind: events.KindMarketData, TraceID: "trace-123", RoutingVersion: 7}
	err := fanout.Dispatch(context.Background(), original, subscribers)
	if err == nil {
		t.Fatalf("expected aggregated error")
	}
	if !strings.Contains(err.Error(), errBoom.Error()) {
		t.Fatalf("aggregated error missing subscriber failure: %v", err)
	}
	if !recycler.WasRecycled(original) {
		t.Fatalf("expected original event to be recycled")
	}
	msg, fields := logger.Snapshot()
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
}

func TestFanoutRecoversSubscriberPanic(t *testing.T) {
	recycler, pool := newTrackingRecycler(t)
	fanout := dispatcher.NewFanout(recycler, pool, nil, 4)

	subscriber := dispatcher.Subscriber{
		ID: "panic",
		Deliver: func(ctx context.Context, ev *events.Event) error {
			defer recycler.RecycleEvent(ev)
			panic("boom")
		},
	}

	original := &events.Event{Kind: events.KindMarketData, TraceID: "trace-panic"}
	err := fanout.Dispatch(context.Background(), original, []dispatcher.Subscriber{subscriber, subscriber})
	if err == nil {
		t.Fatalf("expected panic to be recovered as error")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Fatalf("expected aggregated error to mention panic, got %v", err)
	}
	if !recycler.WasRecycled(original) {
		t.Fatalf("expected original event to be recycled after panic")
	}
}

func TestFanoutPropagatesContextCancellation(t *testing.T) {
	recycler, pool := newTrackingRecycler(t)
	fanout := dispatcher.NewFanout(recycler, pool, nil, 4)

	subscribers := []dispatcher.Subscriber{
		{ID: "a", Deliver: func(ctx context.Context, ev *events.Event) error {
			defer recycler.RecycleEvent(ev)
			time.Sleep(5 * time.Millisecond)
			return nil
		}},
		{ID: "b", Deliver: func(ctx context.Context, ev *events.Event) error {
			defer recycler.RecycleEvent(ev)
			time.Sleep(10 * time.Millisecond)
			return nil
		}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(1 * time.Millisecond)
		cancel()
	}()

	original := &events.Event{Kind: events.KindMarketData, TraceID: "trace-context"}
	err := fanout.Dispatch(ctx, original, subscribers)
	if err == nil {
		t.Fatalf("expected context cancellation error")
	}
	if !strings.Contains(err.Error(), "context error") {
		t.Fatalf("expected aggregated error to include context error, got %v", err)
	}
	if !recycler.WasRecycled(original) {
		t.Fatalf("expected original event to be recycled after cancellation")
	}
}
