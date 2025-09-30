package dispatcher

import (
	"context"
	"time"

	"github.com/coachpo/meltica/config"
	"github.com/coachpo/meltica/internal/bus/databus"
	"github.com/coachpo/meltica/internal/observability"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

// Runtime coordinates dispatcher ingestion and delivery.
type Runtime struct {
	bus            databus.Bus
	pools          *pool.PoolManager
	cfg            config.DispatcherRuntimeConfig
	metrics        *observability.RuntimeMetrics
	ordering       *StreamOrdering
	clock          func() time.Time
	dedupe         map[string]time.Time
	dedupeWindow   time.Duration
	dedupeCapacity int
}

// NewRuntime constructs a dispatcher runtime instance.
func NewRuntime(bus databus.Bus, pools *pool.PoolManager, cfg config.DispatcherRuntimeConfig, metrics *observability.RuntimeMetrics) *Runtime {
	if metrics == nil {
		metrics = observability.NewRuntimeMetrics()
	}
	clock := time.Now
	ordering := NewStreamOrdering(cfg.StreamOrdering, clock)
	runtime := new(Runtime)
	runtime.bus = bus
	runtime.pools = pools
	runtime.cfg = cfg
	runtime.metrics = metrics
	runtime.ordering = ordering
	runtime.clock = clock
	runtime.dedupe = make(map[string]time.Time, 1024)
	runtime.dedupeWindow = 5 * time.Minute
	runtime.dedupeCapacity = 8192
	return runtime
}

// Start consumes canonical events and delivers them onto the data bus until the context is cancelled.
func (r *Runtime) Start(ctx context.Context, events <-chan *schema.Event) <-chan error {
	errCh := make(chan error, 4)
	go r.run(ctx, events, errCh)
	return errCh
}

func (r *Runtime) run(ctx context.Context, events <-chan *schema.Event, errCh chan<- error) {
	flushInterval := r.cfg.StreamOrdering.FlushInterval
	if flushInterval <= 0 {
		flushInterval = 50 * time.Millisecond
	}
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	defer close(errCh)

	publish := func(batch []*schema.Event) {
		for i, evt := range batch {
			if evt == nil {
				batch[i] = nil
				continue
			}
			if evt.Provider == "" {
				evt.Provider = "binance"
			}
			clone := cloneEventForFanOut(evt)
			if clone != nil {
				if err := r.bus.Publish(ctx, clone); err != nil {
					select {
					case errCh <- err:
					default:
					}
				}
			}
			r.releaseEvent(evt)
			batch[i] = nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			publish(r.ordering.Flush(r.clock()))
			return
		case evt, ok := <-events:
			if !ok {
				publish(r.ordering.Flush(r.clock()))
				return
			}
			if evt == nil {
				continue
			}
			if !r.markSeen(evt.EventID) {
				r.releaseEvent(evt)
				continue
			}
			ready, buffered := r.ordering.OnEvent(evt)
			if !buffered {
				r.releaseEvent(evt)
			}
			if r.metrics != nil {
				key := StreamKey{Provider: evt.Provider, Symbol: evt.Symbol, EventType: evt.Type}
				r.metrics.RecordBufferDepth(key.String(), r.ordering.Depth(key))
			}
			publish(ready)
		case <-ticker.C:
			publish(r.ordering.Flush(r.clock()))
		}
	}
}

func (r *Runtime) markSeen(eventID string) bool {
	if eventID == "" {
		return true
	}
	now := r.clock().UTC()
	if ts, ok := r.dedupe[eventID]; ok {
		if now.Sub(ts) < r.dedupeWindow {
			return false
		}
	}
	r.dedupe[eventID] = now
	if len(r.dedupe) > r.dedupeCapacity {
		r.gcDedupe(now)
	}
	return true
}

func (r *Runtime) gcDedupe(now time.Time) {
	threshold := now.Add(-r.dedupeWindow)
	for id, ts := range r.dedupe {
		if ts.Before(threshold) {
			delete(r.dedupe, id)
		}
	}
}

func (r *Runtime) releaseEvent(evt *schema.Event) {
	if evt == nil || r.pools == nil {
		return
	}
	r.pools.Put("CanonicalEvent", evt)
}
