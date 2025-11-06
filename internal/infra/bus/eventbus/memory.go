package eventbus

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	concpool "github.com/sourcegraph/conc/pool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/coachpo/meltica/internal/domain/errs"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/pool"
	"github.com/coachpo/meltica/internal/infra/telemetry"
)

// MemoryBus is an in-memory implementation of the data bus.
type MemoryBus struct {
	cfg MemoryConfig

	ctx    context.Context
	cancel context.CancelFunc
	pools  *pool.PoolManager

	mu           sync.RWMutex
	subscribers  map[schema.EventType]map[SubscriptionID]*subscriber
	shutdownOnce sync.Once
	nextID       uint64
	workers      int

	eventsPublishedCounter metric.Int64Counter
	subscriberGauge        metric.Int64UpDownCounter
	deliveryErrorCounter   metric.Int64Counter
	fanoutHistogram        metric.Int64Histogram
	publishDuration        metric.Float64Histogram
	deliveryBlockedCounter metric.Int64Counter
}

type subscriber struct {
	ctx    context.Context
	cancel context.CancelFunc
	ch     chan *schema.Event
	once   sync.Once
}

// NewMemoryBus constructs a memory-backed data bus.
func NewMemoryBus(cfg MemoryConfig) *MemoryBus {
	cfg = cfg.normalize()
	ctx, cancel := context.WithCancel(context.Background())
	bus := new(MemoryBus)
	bus.cfg = cfg
	bus.ctx = ctx
	bus.cancel = cancel
	bus.pools = cfg.Pools
	bus.subscribers = make(map[schema.EventType]map[SubscriptionID]*subscriber)
	bus.workers = cfg.FanoutWorkers

	meter := otel.Meter("eventbus")
	bus.eventsPublishedCounter, _ = meter.Int64Counter("eventbus.events.published",
		metric.WithDescription("Number of events published to the bus"),
		metric.WithUnit("{event}"))
	bus.subscriberGauge, _ = meter.Int64UpDownCounter("eventbus.subscribers",
		metric.WithDescription("Number of active subscribers"),
		metric.WithUnit("{subscriber}"))
	bus.deliveryErrorCounter, _ = meter.Int64Counter("eventbus.delivery.errors",
		metric.WithDescription("Number of event delivery errors"),
		metric.WithUnit("{error}"))
	bus.fanoutHistogram, _ = meter.Int64Histogram("eventbus.fanout.size",
		metric.WithDescription("Number of subscribers per fanout"),
		metric.WithUnit("{subscriber}"))
	bus.publishDuration, _ = meter.Float64Histogram("eventbus.publish.duration",
		metric.WithDescription("Latency of eventbus publish operations"),
		metric.WithUnit("ms"))
	bus.deliveryBlockedCounter, _ = meter.Int64Counter("eventbus.delivery.blocked",
		metric.WithDescription("Number of deliveries dropped due to subscriber backpressure"),
		metric.WithUnit("{event}"))

	return bus
}

// Publish fan-outs the event to all subscribers of its type.
// Route-first: counts subscribers before any pool work, short-circuits when n==0.
func (b *MemoryBus) Publish(ctx context.Context, evt *schema.Event) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if evt == nil {
		return nil
	}
	if evt.Type == "" {
		return errs.New("eventbus/publish", errs.CodeInvalid, errs.WithMessage("event type required"))
	}

	provider := evt.Provider
	symbol := evt.Symbol
	eventType := string(evt.Type)
	start := time.Now()
	result := "success"

	defer func() {
		if b.publishDuration != nil {
			attrs := telemetry.OperationResultAttributes(telemetry.Environment(), provider, "eventbus.publish", result)
			if eventType != "" {
				attrs = append(attrs, telemetry.AttrEventType.String(eventType))
			}
			if symbol != "" {
				attrs = append(attrs, telemetry.AttrSymbol.String(symbol))
			}
			b.publishDuration.Record(ctx, float64(time.Since(start).Milliseconds()), metric.WithAttributes(attrs...))
		}
	}()

	// ROUTE FIRST: snapshot subscribers before any pool operations.
	b.mu.RLock()
	subMap := b.subscribers[evt.Type]
	n := len(subMap)
	subscribers := make([]*subscriber, 0, n)
	for _, sub := range subMap {
		subscribers = append(subscribers, sub)
	}
	b.mu.RUnlock()

	if b.fanoutHistogram != nil {
		b.fanoutHistogram.Record(ctx, int64(n), metric.WithAttributes(
			attribute.String("environment", telemetry.Environment()),
			attribute.String("event_type", string(evt.Type)),
			attribute.String("provider", evt.Provider),
			attribute.String("symbol", evt.Symbol)))
	}

	// SHORT-CIRCUIT: no subscribers means no pool work, no delivery.
	if n == 0 {
		result = "no_subscribers"
		b.recycle(evt)
		return nil
	}

	// ALLOCATE-IF-SOME: pre-borrow exactly n clones for n subscribers.
	clones, err := b.borrowBatchForFanout(ctx, evt, n)
	if err != nil {
		// Source event not cloned; bus must recycle it.
		b.recycle(evt)
		if b.deliveryErrorCounter != nil {
			b.deliveryErrorCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("environment", telemetry.Environment()),
				attribute.String("error", "clone_batch_failed"),
				attribute.String("event_type", string(evt.Type)),
				attribute.String("provider", evt.Provider),
				attribute.String("symbol", evt.Symbol)))
		}
		result = "clone_batch_failed"
		return err
	}

	// DELIVER: dispatch with pre-borrowed clones.
	// dispatch handles recycling: deliverWithRecycle recycles on failure,
	// and dispatch recycles unused clones on early validation errors.
	if err := b.dispatch(ctx, subscribers, clones); err != nil {
		// Source event must still be recycled; clones already handled by dispatch.
		b.recycle(evt)
		if b.deliveryErrorCounter != nil {
			b.deliveryErrorCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("environment", telemetry.Environment()),
				attribute.String("error", "dispatch_failed"),
				attribute.String("event_type", string(evt.Type)),
				attribute.String("provider", evt.Provider),
				attribute.String("symbol", evt.Symbol)))
		}
		result = "dispatch_failed"
		return err
	}

	if b.eventsPublishedCounter != nil {
		b.eventsPublishedCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("environment", telemetry.Environment()),
			attribute.String("event_type", string(evt.Type)),
			attribute.String("provider", evt.Provider),
			attribute.String("symbol", evt.Symbol)))
	}

	// Source event is no longer needed; recycle it.
	b.recycle(evt)
	return nil
}

// Subscribe registers for events of the given type and returns a subscription ID and channel.
func (b *MemoryBus) Subscribe(ctx context.Context, typ schema.EventType) (SubscriptionID, <-chan *schema.Event, error) {
	if typ == "" {
		return "", nil, errs.New("eventbus/subscribe", errs.CodeInvalid, errs.WithMessage("event type required"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)

	sub := new(subscriber)
	sub.ctx = ctx
	sub.cancel = cancel
	sub.ch = make(chan *schema.Event, b.cfg.BufferSize)

	id := SubscriptionID(fmt.Sprintf("sub-%d", atomic.AddUint64(&b.nextID, 1)))

	b.mu.Lock()
	if _, ok := b.subscribers[typ]; !ok {
		b.subscribers[typ] = make(map[SubscriptionID]*subscriber)
	}
	b.subscribers[typ][id] = sub
	b.mu.Unlock()

	if b.subscriberGauge != nil {
		b.subscriberGauge.Add(ctx, 1, metric.WithAttributes(
			attribute.String("environment", telemetry.Environment()),
			attribute.String("event_type", string(typ))))
	}

	go b.observe(typ, id, sub)
	return id, sub.ch, nil
}

// Unsubscribe removes the subscription and closes the channel.
func (b *MemoryBus) Unsubscribe(id SubscriptionID) {
	if id == "" {
		return
	}
	b.mu.Lock()
	for typ, subs := range b.subscribers {
		if sub, ok := subs[id]; ok {
			delete(subs, id)
			if len(subs) == 0 {
				delete(b.subscribers, typ)
			}
			b.mu.Unlock()
			if b.subscriberGauge != nil {
				b.subscriberGauge.Add(context.Background(), -1, metric.WithAttributes(
					attribute.String("environment", telemetry.Environment()),
					attribute.String("event_type", string(typ))))
			}
			sub.close()
			return
		}
	}
	b.mu.Unlock()
}

// Close shuts down the bus and all subscriptions.
func (b *MemoryBus) Close() {
	b.shutdownOnce.Do(func() {
		b.cancel()
		b.mu.Lock()
		for typ, subs := range b.subscribers {
			for id, sub := range subs {
				if sub != nil {
					sub.close()
				}
				delete(subs, id)
			}
			delete(b.subscribers, typ)
		}
		b.mu.Unlock()
	})
}

func (b *MemoryBus) observe(typ schema.EventType, id SubscriptionID, sub *subscriber) {
	<-sub.ctx.Done()
	b.mu.Lock()
	subs := b.subscribers[typ]
	if subs != nil {
		if stored, ok := subs[id]; ok && stored == sub {
			delete(subs, id)
			if len(subs) == 0 {
				delete(b.subscribers, typ)
			}
		}
	}
	b.mu.Unlock()
	sub.close()
}

// deliverWithRecycle delivers an event and recycles it only on failure paths.
// On success ownership transfers to the subscriber.
func (b *MemoryBus) deliverWithRecycle(ctx context.Context, sub *subscriber, evt *schema.Event) error {
	if err := sub.ctx.Err(); err != nil {
		b.recycle(evt)
		return fmt.Errorf("subscriber context: %w", err)
	}
	select {
	case <-b.ctx.Done():
		b.recycle(evt)
		return errs.New("eventbus/publish", errs.CodeUnavailable, errs.WithMessage("bus closed"))
	case <-ctx.Done():
		b.recycle(evt)
		return fmt.Errorf("deliver context: %w", ctx.Err())
	case <-sub.ctx.Done():
		b.recycle(evt)
		return nil
	case sub.ch <- evt:
		return nil
	default:
		var dropped *schema.Event
		select {
		case dropped = <-sub.ch:
		default:
		}
		if dropped != nil {
			b.recycle(dropped)
		}
		log.Printf("eventbus: subscriber buffer full; dropped oldest event type=%s provider=%s symbol=%s", evt.Type, evt.Provider, evt.Symbol)
		if b.deliveryBlockedCounter != nil {
			attrs := telemetry.EventAttributes(telemetry.Environment(), string(evt.Type), evt.Provider, evt.Symbol)
			b.deliveryBlockedCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
		}
		select {
		case sub.ch <- evt:
			return nil
		default:
			b.recycle(evt)
			return errs.New("eventbus/publish", errs.CodeUnavailable, errs.WithMessage("subscriber buffer full"))
		}
	}
}

// dispatch delivers pre-borrowed clones to subscribers.
// Each subscriber gets its dedicated clone; deliverWithRecycle handles recycling.
func (b *MemoryBus) dispatch(ctx context.Context, subs []*subscriber, clones []*schema.Event) error {
	if len(subs) == 0 {
		// No subscribers, recycle all pre-borrowed clones.
		b.pools.ReturnEventInsts(clones)
		return nil
	}
	if len(clones) != len(subs) {
		// Validation failure, recycle all clones before returning error.
		b.pools.ReturnEventInsts(clones)
		err := fmt.Errorf("eventbus/dispatch: clone count (%d) != subscriber count (%d)", len(clones), len(subs))
		return err
	}

	workerLimit := b.workers
	if workerLimit <= 0 {
		workerLimit = 1
	}

	p := concpool.New().WithMaxGoroutines(workerLimit)
	errCh := make(chan error, len(subs))

	for idx, subscriber := range subs {
		if subscriber == nil {
			// Recycle the unused clone for this nil subscriber.
			b.recycle(clones[idx])
			continue
		}
		i := idx
		sub := subscriber
		clone := clones[i]
		p.Go(func() {
			if err := b.deliverWithRecycle(ctx, sub, clone); err != nil {
				errCh <- err
			}
		})
	}

	p.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *MemoryBus) recycle(evt *schema.Event) {
	if evt == nil {
		return
	}
	if b.pools != nil {
		b.pools.ReturnEventInst(evt)
	}
}

// borrowBatchForFanout pre-allocates exactly n clones from the pool.
// Returns the slice of clones and any error.
// Caller (dispatch) is responsible for recycling via deliverWithRecycle.
func (b *MemoryBus) borrowBatchForFanout(ctx context.Context, src *schema.Event, n int) ([]*schema.Event, error) {
	if b.pools == nil {
		return nil, fmt.Errorf("eventbus/publish: canonical event pool unavailable")
	}
	if src == nil {
		return nil, fmt.Errorf("eventbus/publish: source event is nil")
	}
	if n <= 0 {
		return nil, nil
	}

	clones, err := b.pools.BorrowEventInsts(ctx, n)
	if err != nil {
		return nil, fmt.Errorf("eventbus/publish: borrow batch: %w", err)
	}
	if len(clones) != n {
		b.pools.ReturnEventInsts(clones)
		return nil, fmt.Errorf("eventbus/publish: expected %d clones, got %d", n, len(clones))
	}

	// Copy source payload into each clone.
	for _, clone := range clones {
		if clone == nil {
			b.pools.ReturnEventInsts(clones)
			return nil, fmt.Errorf("eventbus/publish: nil clone in batch")
		}
		schema.CopyEvent(clone, src)
	}

	return clones, nil
}

func (s *subscriber) close() {
	s.once.Do(func() {
		s.cancel()
		close(s.ch)
	})
}

// PoolManager exposes the underlying pool manager used for event allocations.
func (b *MemoryBus) PoolManager() *pool.PoolManager {
	if b == nil {
		return nil
	}
	return b.pools
}
