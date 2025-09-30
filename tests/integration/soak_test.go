package integration

import (
	"context"
	"runtime"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/coachpo/meltica/core/consumer"
	"github.com/coachpo/meltica/core/events"
	"github.com/coachpo/meltica/core/recycler"
)

func TestRecyclerSoakNoMemoryGrowth(t *testing.T) {
	eventPool := &sync.Pool{New: func() any { return &events.Event{} }}
	mergedPool := &sync.Pool{New: func() any { return &events.MergedEvent{} }}
	execPool := &sync.Pool{New: func() any { return &events.ExecReport{} }}
	recyclerMetrics := recycler.NewRecyclerMetrics(prometheus.NewRegistry())
	rec := recycler.NewRecycler(eventPool, mergedPool, execPool, recyclerMetrics)
	meter := consumer.NewConsumerMetrics(prometheus.NewRegistry())
	wrapper := consumer.NewWrapper("soak-consumer", rec, meter)

	ctx := context.Background()
	const iterations = 1_000_000

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	for i := 0; i < iterations; i++ {
		ev := &events.Event{Kind: events.KindMarketData, RoutingVersion: uint64(i % 1024)}
		if err := wrapper.Invoke(ctx, ev, func(ctx context.Context, event *events.Event) error {
			return nil
		}); err != nil {
			t.Fatalf("invoke returned error at iteration %d: %v", i, err)
		}
	}

	runtime.GC()
	runtime.ReadMemStats(&after)
	const maxIncrease = 16 << 20 // 16 MiB allowance for allocator fluctuations
	if after.Alloc > before.Alloc+maxIncrease {
		t.Fatalf("expected alloc increase <= %d bytes, before=%d after=%d", maxIncrease, before.Alloc, after.Alloc)
	}

	if invocations := testutil.ToFloat64(meter.InvocationsCounter("soak-consumer")); invocations != iterations {
		t.Fatalf("expected %d invocations recorded, got %.0f", iterations, invocations)
	}
}
