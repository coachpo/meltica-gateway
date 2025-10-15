// Package dispatcher implements fan-out logic for event delivery.
package dispatcher

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sourcegraph/conc/pool"

	"github.com/coachpo/meltica/pkg/events"
	"github.com/coachpo/meltica/pkg/recycler"
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

// FanoutError aggregates multiple subscriber errors with contextual metadata.
type FanoutError struct {
	Operation         string
	TraceID           string
	EventKind         events.EventKind
	RoutingVersion    uint64
	SubscriberCount   int
	FailedSubscribers []string
	Errors            []error
}

// Error returns a descriptive summary of the aggregated fan-out failure.
func (e *FanoutError) Error() string {
	if e == nil {
		return "<nil>"
	}
	parts := []string{}
	if op := strings.TrimSpace(e.Operation); op != "" {
		parts = append(parts, op)
	} else {
		parts = append(parts, "fanout error")
	}
	if e.TraceID != "" {
		parts = append(parts, fmt.Sprintf("trace_id=%s", e.TraceID))
	}
	if e.EventKind != 0 {
		parts = append(parts, fmt.Sprintf("event_kind=%s", e.EventKind.String()))
	}
	if e.RoutingVersion > 0 {
		parts = append(parts, fmt.Sprintf("routing_version=%d", e.RoutingVersion))
	}
	if e.SubscriberCount > 0 {
		parts = append(parts, fmt.Sprintf("subscriber_count=%d", e.SubscriberCount))
	}
	if len(e.FailedSubscribers) > 0 {
		parts = append(parts, fmt.Sprintf("failed_subscribers=%v", e.FailedSubscribers))
	}
	for _, err := range e.Errors {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	return strings.Join(parts, ": ")
}

// Unwrap exposes the underlying subscriber errors for errors.Is/As compatibility.
func (e *FanoutError) Unwrap() []error {
	if e == nil {
		return nil
	}
	return append([]error(nil), e.Errors...)
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
	return &FanoutError{
		Operation:         "dispatcher fan-out",
		TraceID:           original.TraceID,
		EventKind:         original.Kind,
		RoutingVersion:    original.RoutingVersion,
		SubscriberCount:   count,
		FailedSubscribers: uniqueStrings(failedSubscribers),
		Errors:            append([]error(nil), workerErrs...),
	}
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
