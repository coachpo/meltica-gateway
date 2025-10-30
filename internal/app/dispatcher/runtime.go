package dispatcher

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/bus/eventbus"
	"github.com/coachpo/meltica/internal/infra/pool"
	"github.com/coachpo/meltica/internal/infra/telemetry"
)

// Runtime coordinates dispatcher ingestion and delivery.
type Runtime struct {
	bus            eventbus.Bus
	table          *Table
	pools          *pool.PoolManager
	clock          func() time.Time
	dedupe         map[string]time.Time
	dedupeWindow   time.Duration
	dedupeCapacity int

	eventsIngestedCounter  metric.Int64Counter
	eventsDroppedCounter   metric.Int64Counter
	eventsDuplicateCounter metric.Int64Counter
	processingDuration     metric.Float64Histogram
	routingRevisionGauge   metric.Int64ObservableGauge
}

// NewRuntime constructs a dispatcher runtime instance.
func NewRuntime(bus eventbus.Bus, table *Table, pools *pool.PoolManager) *Runtime {
	clock := time.Now
	runtime := new(Runtime)
	runtime.bus = bus
	runtime.table = table
	runtime.pools = pools
	runtime.clock = clock
	runtime.dedupe = make(map[string]time.Time, 1024)
	runtime.dedupeWindow = 5 * time.Minute
	runtime.dedupeCapacity = 8192

	meter := otel.Meter("dispatcher")
	runtime.eventsIngestedCounter, _ = meter.Int64Counter("dispatcher.events.ingested",
		metric.WithDescription("Number of events ingested by dispatcher"),
		metric.WithUnit("{event}"))
	runtime.eventsDroppedCounter, _ = meter.Int64Counter("dispatcher.events.dropped",
		metric.WithDescription("Number of events dropped"),
		metric.WithUnit("{event}"))
	runtime.eventsDuplicateCounter, _ = meter.Int64Counter("dispatcher.events.duplicate",
		metric.WithDescription("Number of duplicate events detected"),
		metric.WithUnit("{event}"))
	runtime.processingDuration, _ = meter.Float64Histogram("dispatcher.processing.duration",
		metric.WithDescription("Event processing duration"),
		metric.WithUnit("ms"))
	runtime.routingRevisionGauge, _ = meter.Int64ObservableGauge("dispatcher.routing.revision",
		metric.WithDescription("Current routing table revision counter"),
		metric.WithUnit("{version}"),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			if runtime.table != nil {
				observer.Observe(runtime.table.Version())
			}
			return nil
		}))

	return runtime
}

// Start consumes canonical events and delivers them onto the data bus until the context is cancelled.
func (r *Runtime) Start(ctx context.Context, events <-chan *schema.Event) <-chan error {
	errCh := make(chan error, 4)
	go r.run(ctx, events, errCh)
	return errCh
}

func (r *Runtime) run(ctx context.Context, events <-chan *schema.Event, errCh chan<- error) {
	defer close(errCh)

	publish := func(batch []*schema.Event) {
		if len(batch) == 0 {
			return
		}

		for i, evt := range batch {
			if evt == nil {
				batch[i] = nil
				continue
			}
			if evt.Provider == "" {
				evt.Provider = "binance"
			}
			// Pass original event to bus; bus handles routing and cloning
			if err := r.bus.Publish(ctx, evt); err != nil {
				select {
				case errCh <- err:
				default:
				}
			}
			batch[i] = nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			if evt == nil {
				continue
			}

			start := r.clock()

			if r.eventsIngestedCounter != nil {
				r.eventsIngestedCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("environment", telemetry.Environment()),
					attribute.String("event_type", string(evt.Type)),
					attribute.String("provider", evt.Provider),
					attribute.String("symbol", evt.Symbol)))
			}

			if rv := r.currentRoutingVersion(); rv > 0 {
				evt.RoutingVersion = rv
			}
			if evt.EmitTS.IsZero() {
				evt.EmitTS = r.clock().UTC()
			}
			if !r.markSeen(evt.EventID) {
				if r.eventsDuplicateCounter != nil {
					r.eventsDuplicateCounter.Add(ctx, 1, metric.WithAttributes(
						attribute.String("environment", telemetry.Environment()),
						attribute.String("event_type", string(evt.Type)),
						attribute.String("provider", evt.Provider),
						attribute.String("symbol", evt.Symbol)))
				}
				r.releaseEvent(evt)
				continue
			}
			// Pass-through mode: Publish immediately without reordering
			// BookAssembler already handles ordering for orderbook events
			// Ticker/Trade events don't require strict ordering
			ready := []*schema.Event{evt}

			duration := r.clock().Sub(start).Milliseconds()
			if r.processingDuration != nil {
				r.processingDuration.Record(ctx, float64(duration), metric.WithAttributes(
					attribute.String("environment", telemetry.Environment()),
					attribute.String("event_type", string(evt.Type)),
					attribute.String("provider", evt.Provider),
					attribute.String("symbol", evt.Symbol)))
			}

			publish(ready)
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

func (r *Runtime) currentRoutingVersion() int {
	if r.table == nil {
		return 0
	}
	return int(r.table.Version())
}

func (r *Runtime) releaseEvent(evt *schema.Event) {
	if r.pools != nil {
		r.pools.ReturnEventInst(evt)
	}
}
