// Package dispatcher implements fan-out logic for event delivery.
package dispatcher

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/sourcegraph/conc/pool"

	"github.com/coachpo/meltica/core/events"
	"github.com/coachpo/meltica/core/recycler"
	"github.com/coachpo/meltica/internal/observability"
)

// deliveryPool defines the subset of sync.Pool behaviour used for event duplication.
type deliveryPool interface {
	Get() any
	Put(any)
}

// DeliveryFunc represents the subscriber handler invoked for each fan-out duplicate.
type DeliveryFunc func(context.Context, *events.Event) error

// Subscriber encapsulates metadata and handler for an event consumer.
type Subscriber struct {
	ID      string
	Deliver DeliveryFunc
}

// Fanout coordinates duplicate creation and parallel dispatch to subscribers.
type Fanout struct {
	recycler   recycler.Recycler
	duplicates deliveryPool
	metrics    *FanoutMetrics
	maxWorkers int
}

// NewFanout constructs a fan-out dispatcher with the provided recycler, duplicate pool, metrics, and concurrency limit.
func NewFanout(rec recycler.Recycler, pool deliveryPool, metrics *FanoutMetrics, maxWorkers int) *Fanout {
	if maxWorkers <= 0 {
		maxWorkers = runtime.GOMAXPROCS(0)
	}
	if pool == nil {
		pool = &sync.Pool{New: func() any {
			return &events.Event{} //nolint:exhaustruct
		}}
	}
	return &Fanout{
		recycler:   rec,
		duplicates: pool,
		metrics:    metrics,
		maxWorkers: maxWorkers,
	}
}

// Dispatch delivers the event to all subscribers using pooled duplicates and structured concurrency.
func (f *Fanout) Dispatch(ctx context.Context, original *events.Event, subscribers []Subscriber) error {
	if original == nil {
		return nil
	}
	count := len(subscribers)
	if count == 0 {
		if f.recycler != nil {
			f.recycler.RecycleEvent(original)
		}
		return nil
	}
	if count == 1 {
		sub := subscribers[0]
		if sub.Deliver == nil {
			return nil
		}
		return sub.Deliver(ctx, original)
	}
	workerLimit := f.maxWorkers
	if workerLimit > count {
		workerLimit = count
	}
	perDurations := make([]time.Duration, count)
	start := time.Now()
	var mu sync.Mutex
	var workerErrs []error
	var failedSubscribers []string
	p := pool.New().WithMaxGoroutines(workerLimit)
	for idx, subscriber := range subscribers {
		i := idx
		if subscriber.Deliver == nil {
			perDurations[i] = 0
			continue
		}
		sub := subscriber
		p.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					workerErrs = append(workerErrs, fmt.Errorf("subscriber %s panic: %v", sub.ID, r))
					failedSubscribers = append(failedSubscribers, sub.ID)
					mu.Unlock()
				}
			}()
			if err := ctx.Err(); err != nil {
				mu.Lock()
				workerErrs = append(workerErrs, fmt.Errorf("context error: %w", err))
				failedSubscribers = append(failedSubscribers, "context")
				mu.Unlock()
				return
			}
			dup := f.borrowDuplicate()
			if dup == nil {
				if err := sub.Deliver(ctx, nil); err != nil {
					mu.Lock()
					workerErrs = append(workerErrs, fmt.Errorf("subscriber %s: %w", sub.ID, err))
					failedSubscribers = append(failedSubscribers, sub.ID)
					mu.Unlock()
				}
				return
			}
			cloneEvent(original, dup)
			deliveryStart := time.Now()
			if err := sub.Deliver(ctx, dup); err != nil {
				mu.Lock()
				workerErrs = append(workerErrs, fmt.Errorf("subscriber %s: %w", sub.ID, err))
				failedSubscribers = append(failedSubscribers, sub.ID)
				mu.Unlock()
			}
			perDurations[i] = time.Since(deliveryStart)
		})
	}
	p.Wait()
	if err := ctx.Err(); err != nil {
		mu.Lock()
		workerErrs = append(workerErrs, fmt.Errorf("context error: %w", err))
		failedSubscribers = append(failedSubscribers, "context")
		mu.Unlock()
	}
	total := time.Since(start)
	if f.metrics != nil {
		f.metrics.Observe(count, perDurations, total)
	}
	if f.recycler != nil {
		f.recycler.RecycleEvent(original)
	}
	if len(workerErrs) == 0 {
		return nil
	}
	//nolint:wrapcheck // aggregation already returns contextualized error
	return observability.AggregateErrors(
		"dispatcher fan-out",
		workerErrs,
		observability.Field{Key: "trace_id", Value: original.TraceID},
		observability.Field{Key: "event_kind", Value: original.Kind.String()},
		observability.Field{Key: "routing_version", Value: original.RoutingVersion},
		observability.Field{Key: "subscriber_count", Value: count},
		observability.Field{Key: "failed_subscribers", Value: uniqueStrings(failedSubscribers)},
	) //nolint:wrapcheck
}

func (f *Fanout) borrowDuplicate() *events.Event {
	var dup *events.Event
	if f.duplicates != nil {
		if v := f.duplicates.Get(); v != nil {
			if ev, ok := v.(*events.Event); ok {
				dup = ev
			}
		}
	}
	if dup == nil {
		dup = &events.Event{} //nolint:exhaustruct
	}
	if f.recycler != nil {
		f.recycler.CheckoutEvent(dup)
	}
	dup.Reset()
	return dup
}

// cloneEvent copies all relevant fields from the source to the destination event.
func cloneEvent(src, dst *events.Event) {
	if src == nil || dst == nil {
		return
	}
	dst.TraceID = src.TraceID
	dst.RoutingVersion = src.RoutingVersion
	dst.Kind = src.Kind
	dst.Payload = src.Payload
	dst.IngestTS = src.IngestTS
	dst.SeqProvider = src.SeqProvider
	dst.ProviderID = src.ProviderID
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	return result
}
