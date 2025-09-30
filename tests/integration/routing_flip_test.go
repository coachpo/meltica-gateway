package integration

import (
	"context"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/coachpo/meltica/core/consumer"
	"github.com/coachpo/meltica/core/events"
	"github.com/coachpo/meltica/core/recycler"
)

func newConsumerWrapper(t testing.TB, consumerID string) (*consumer.Wrapper, *consumer.ConsumerMetrics) {
	t.Helper()
	eventPool := &sync.Pool{New: func() any { return &events.Event{} }}
	mergedPool := &sync.Pool{New: func() any { return &events.MergedEvent{} }}
	execPool := &sync.Pool{New: func() any { return &events.ExecReport{} }}
	metrics := recycler.NewRecyclerMetrics(prometheus.NewRegistry())
	rec := recycler.NewRecycler(eventPool, mergedPool, execPool, metrics)
	meter := consumer.NewConsumerMetrics(prometheus.NewRegistry())
	return consumer.NewWrapper(consumerID, rec, meter), meter
}

func TestRoutingFlipFiltersMarketDataButDeliversCritical(t *testing.T) {
	ctx := context.Background()
	wrapper, meter := newConsumerWrapper(t, "flip-consumer")
	wrapper.UpdateMinVersion(100)

	var deliveredMarketData int
	var deliveredCritical int

	send := func(kind events.EventKind, version uint64) {
		ev := &events.Event{Kind: kind, RoutingVersion: version}
		err := wrapper.Invoke(ctx, ev, func(ctx context.Context, event *events.Event) error {
			switch event.Kind {
			case events.KindMarketData:
				deliveredMarketData++
			case events.KindExecReport, events.KindControlAck, events.KindControlResult:
				deliveredCritical++
			}
			return nil
		})
		if err != nil {
			t.Fatalf("invoke returned error: %v", err)
		}
	}

	// Send 100 market data events with stale version (< min accept).
	for i := 0; i < 100; i++ {
		send(events.KindMarketData, 50)
	}

	// Send critical events with stale version; they must still be delivered.
	criticalKinds := []events.EventKind{events.KindExecReport, events.KindControlAck, events.KindControlResult}
	for _, kind := range criticalKinds {
		for i := 0; i < 5; i++ {
			send(kind, 50)
		}
	}

	if deliveredMarketData != 0 {
		t.Fatalf("expected 0 market data deliveries, got %d", deliveredMarketData)
	}
	if deliveredCritical != 15 {
		t.Fatalf("expected 15 critical deliveries, got %d", deliveredCritical)
	}

	filtered := testutil.ToFloat64(meter.FilteredCounter("flip-consumer"))
	if filtered != 100 {
		t.Fatalf("expected filtered counter to equal stale market data count (100), got %f", filtered)
	}

	invocations := testutil.ToFloat64(meter.InvocationsCounter("flip-consumer"))
	if invocations != 115 {
		t.Fatalf("expected invocations to equal all attempted deliveries (115), got %f", invocations)
	}

	panics := testutil.ToFloat64(meter.PanicCounter("flip-consumer"))
	if panics != 0 {
		t.Fatalf("expected zero panics, got %f", panics)
	}
}

func TestRoutingFlipMaintainsFilteredRatioBelowFivePercent(t *testing.T) {
	ctx := context.Background()
	wrapper, _ := newConsumerWrapper(t, "flip-efficiency")
	wrapper.UpdateMinVersion(200)

	var processedMarketData int
	var totalMarketData int

	send := func(kind events.EventKind, version uint64) {
		ev := &events.Event{Kind: kind, RoutingVersion: version}
		err := wrapper.Invoke(ctx, ev, func(ctx context.Context, event *events.Event) error {
			if event.Kind == events.KindMarketData {
				processedMarketData++
			}
			return nil
		})
		if err != nil {
			t.Fatalf("invoke returned error: %v", err)
		}
	}

	for i := 0; i < 500; i++ {
		totalMarketData++
		if i%100 == 0 {
			send(events.KindMarketData, 250) // new version events allowed
		} else {
			send(events.KindMarketData, 150) // stale events filtered
		}
	}

	allowedRatio := float64(processedMarketData) / float64(totalMarketData)
	if allowedRatio >= 0.05 {
		t.Fatalf("expected <5%% market data during flip, got %.2f%%", allowedRatio*100)
	}
}
