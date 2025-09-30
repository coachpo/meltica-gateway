package tests

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/coachpo/meltica/core/dispatcher"
	"github.com/coachpo/meltica/core/events"
)

func BenchmarkParallelFanoutEfficiency(b *testing.B) {
	recycler, pool := newTrackingRecycler(b)
	metrics := dispatcher.NewFanoutMetrics(prometheus.NewRegistry())
	fanout := dispatcher.NewFanout(recycler, pool, metrics, 16)

	subscribers := make([]dispatcher.Subscriber, 10)
	for i := range subscribers {
		subscribers[i] = dispatcher.Subscriber{
			ID: "bench",
			Deliver: func(ctx context.Context, ev *events.Event) error {
				defer recycler.RecycleEvent(ev)
				time.Sleep(100 * time.Microsecond)
				return nil
			},
		}
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		original := &events.Event{Kind: events.KindMarketData}
		if err := fanout.Dispatch(context.Background(), original, subscribers); err != nil {
			b.Fatalf("fanout dispatch err: %v", err)
		}
	}
}
