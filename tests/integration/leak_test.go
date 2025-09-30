package integration

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.uber.org/goleak"

	"github.com/coachpo/meltica/core/consumer"
	"github.com/coachpo/meltica/core/dispatcher"
	"github.com/coachpo/meltica/core/events"
	"github.com/coachpo/meltica/core/recycler"
)

func TestConsumerWrapperNoGoroutineLeaksUnderPanics(t *testing.T) {
	defer goleak.VerifyNone(t)

	eventPool := &sync.Pool{New: func() any { return &events.Event{} }}
	mergedPool := &sync.Pool{New: func() any { return &events.MergedEvent{} }}
	execPool := &sync.Pool{New: func() any { return &events.ExecReport{} }}
	recyclerMetrics := recycler.NewRecyclerMetrics(prometheus.NewRegistry())
	rec := recycler.NewRecycler(eventPool, mergedPool, execPool, recyclerMetrics)
	meter := consumer.NewConsumerMetrics(prometheus.NewRegistry())
	wrapper := consumer.NewWrapper("leak-consumer", rec, meter)

	ctx := context.Background()
	const total = 100000
	var panicCount int
	for i := 0; i < total; i++ {
		idx := i
		ev := &events.Event{Kind: events.KindMarketData, RoutingVersion: 1}
		err := wrapper.Invoke(ctx, ev, func(ctx context.Context, event *events.Event) error {
			if idx%10 == 0 {
				panic("integration panic")
			}
			return nil
		})
		if idx%10 == 0 {
			panicCount++
			if err == nil {
				t.Fatalf("expected panic error for iteration %d", idx)
			}
			if !strings.Contains(err.Error(), "consumer panic") {
				t.Fatalf("expected consumer panic error, got %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if got := int(testutil.ToFloat64(meter.PanicCounter("leak-consumer"))); got != panicCount {
		t.Fatalf("expected %d panics recorded, got %d", panicCount, got)
	}
}

func TestFanoutNoGoroutineLeaks(t *testing.T) {
	defer goleak.VerifyNone(t)

	eventPool := &sync.Pool{New: func() any { return &events.Event{} }}
	mergedPool := &sync.Pool{New: func() any { return &events.MergedEvent{} }}
	execPool := &sync.Pool{New: func() any { return &events.ExecReport{} }}
	recyclerMetrics := recycler.NewRecyclerMetrics(prometheus.NewRegistry())
	rec := recycler.NewRecycler(eventPool, mergedPool, execPool, recyclerMetrics)
	fanout := dispatcher.NewFanout(rec, eventPool, nil, 8)

	subscribers := []dispatcher.Subscriber{
		{ID: "alpha", Deliver: func(ctx context.Context, ev *events.Event) error {
			if ev != nil {
				rec.RecycleEvent(ev)
			}
			return nil
		}},
		{ID: "beta", Deliver: func(ctx context.Context, ev *events.Event) error {
			if ev != nil {
				rec.RecycleEvent(ev)
			}
			return nil
		}},
	}

	for i := 0; i < 1000; i++ {
		original := &events.Event{Kind: events.KindMarketData, TraceID: fmt.Sprintf("trace-%d", i)}
		if err := fanout.Dispatch(context.Background(), original, subscribers); err != nil {
			t.Fatalf("dispatch returned error: %v", err)
		}
	}
}
