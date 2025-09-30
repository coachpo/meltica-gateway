package integration

import (
	"context"
	"sync"
	"testing"
	"unsafe"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/coachpo/meltica/core/dispatcher"
	"github.com/coachpo/meltica/core/events"
	"github.com/coachpo/meltica/core/recycler"
)

func TestFanoutPoolUtilizationUnderLoadIntegration(t *testing.T) {
	eventPool := &sync.Pool{New: func() any { return &events.Event{} }}
	mergedPool := &sync.Pool{New: func() any { return &events.MergedEvent{} }}
	execPool := &sync.Pool{New: func() any { return &events.ExecReport{} }}
	recyclerMetrics := recycler.NewRecyclerMetrics(prometheus.NewRegistry())
	rec := recycler.NewRecycler(eventPool, mergedPool, execPool, recyclerMetrics)
	metrics := dispatcher.NewFanoutMetrics(prometheus.NewRegistry())
	fanout := dispatcher.NewFanout(rec, eventPool, metrics, 16)

	const subscribers = 8
	const eventCount = 1000

	dupSeen := make(map[uintptr]struct{}, subscribers*eventCount)
	var mu sync.Mutex
	subs := make([]dispatcher.Subscriber, subscribers)
	for i := range subs {
		subs[i] = dispatcher.Subscriber{
			ID: "integration",
			Deliver: func(ctx context.Context, ev *events.Event) error {
				ptr := eventPointer(ev)
				mu.Lock()
				dupSeen[ptr] = struct{}{}
				mu.Unlock()
				rec.RecycleEvent(ev)
				return nil
			},
		}
	}

	for i := 0; i < eventCount; i++ {
		original := &events.Event{Kind: events.KindMarketData}
		if err := fanout.Dispatch(context.Background(), original, subs); err != nil {
			t.Fatalf("dispatch error: %v", err)
		}
	}

	totalDuplicates := subscribers * eventCount
	unique := len(dupSeen)
	utilization := float64(unique) / float64(totalDuplicates)
	if utilization >= 0.8 {
		t.Fatalf("expected <80%% pool utilization, got %.2f (unique=%d total=%d)", utilization*100, unique, totalDuplicates)
	}
}

func eventPointer(ev *events.Event) uintptr {
	if ev == nil {
		return 0
	}
	return uintptr(unsafe.Pointer(ev))
}
