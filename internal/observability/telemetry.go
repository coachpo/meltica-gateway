// Package observability provides lightweight in-memory telemetry primitives.
package observability

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coachpo/meltica/errs"
)

// TelemetrySeverity represents the severity level of a telemetry event.
type TelemetrySeverity string

const (
	// TelemetrySeverityInfo identifies informational telemetry.
	TelemetrySeverityInfo TelemetrySeverity = "INFO"
	// TelemetrySeverityWarn identifies warning telemetry.
	TelemetrySeverityWarn TelemetrySeverity = "WARN"
	// TelemetrySeverityError identifies error telemetry.
	TelemetrySeverityError TelemetrySeverity = "ERROR"
)

// TelemetryEventType enumerates ops-only telemetry event categories.
type TelemetryEventType string

const (
	// TelemetryEventBookResync signals a book resync operation.
	TelemetryEventBookResync TelemetryEventType = "book.resync"
	// TelemetryEventChecksumFailed signals a checksum mismatch event.
	TelemetryEventChecksumFailed TelemetryEventType = "checksum.failed"
	// TelemetryEventMergeSuppressed signals a partial merge suppression.
	TelemetryEventMergeSuppressed TelemetryEventType = "merge.suppressed_partial"
	// TelemetryEventLateEventDropped signals late event drops.
	TelemetryEventLateEventDropped TelemetryEventType = "late_event.dropped"
	// TelemetryEventCoalescingApplied signals coalescing activation.
	TelemetryEventCoalescingApplied TelemetryEventType = "coalescing.applied"
	// TelemetryEventBackpressureApplied signals backpressure enforcement.
	TelemetryEventBackpressureApplied TelemetryEventType = "backpressure.applied"
	// TelemetryEventDLQPublished signals DLQ publication.
	TelemetryEventDLQPublished TelemetryEventType = "dlq.published"
	// TelemetryEventTradingSwitch signals trading mode toggles.
	TelemetryEventTradingSwitch TelemetryEventType = "trading.switch"
)

// TelemetryEvent carries structured observability information for operations.
type TelemetryEvent struct {
	EventID    string             `json:"event_id"`
	Type       TelemetryEventType `json:"type"`
	Severity   TelemetrySeverity  `json:"severity"`
	Timestamp  time.Time          `json:"timestamp"`
	TraceID    string             `json:"trace_id"`
	DecisionID string             `json:"decision_id"`
	Metadata   map[string]any     `json:"metadata"`
}

// TelemetryBus defines pub/sub semantics for telemetry events.
type TelemetryBus interface {
	Publish(ctx context.Context, event TelemetryEvent) error
	Subscribe(ctx context.Context) (<-chan TelemetryEvent, error)
	Close()
}

// InMemoryTelemetryBus is an in-memory implementation of the telemetry bus.
type InMemoryTelemetryBus struct {
	ctx    context.Context
	cancel context.CancelFunc
	buffer int

	mu       sync.RWMutex
	subs     []*telemetrySubscriber
	shutdown sync.Once
}

type telemetrySubscriber struct {
	ctx    context.Context
	cancel context.CancelFunc
	ch     chan TelemetryEvent
	once   sync.Once
}

// NewInMemoryTelemetryBus constructs a memory-backed telemetry bus.
func NewInMemoryTelemetryBus(buffer int) *InMemoryTelemetryBus {
	if buffer <= 0 {
		buffer = 16
	}
	ctx, cancel := context.WithCancel(context.Background())
	bus := new(InMemoryTelemetryBus)
	bus.ctx = ctx
	bus.cancel = cancel
	bus.buffer = buffer
	bus.subs = make([]*telemetrySubscriber, 0)
	return bus
}

// Publish broadcasts the telemetry event to all subscribers.
func (b *InMemoryTelemetryBus) Publish(ctx context.Context, event TelemetryEvent) error {
	if ctx == nil {
		ctx = context.Background()
	}
	b.mu.RLock()
	subs := append([]*telemetrySubscriber(nil), b.subs...)
	b.mu.RUnlock()
	if len(subs) == 0 {
		return nil
	}
	for _, sub := range subs {
		if sub == nil {
			continue
		}
		if err := b.deliver(ctx, sub, event); err != nil {
			return err
		}
	}
	return nil
}

// Subscribe registers a telemetry subscriber.
func (b *InMemoryTelemetryBus) Subscribe(ctx context.Context) (<-chan TelemetryEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	sub := new(telemetrySubscriber)
	sub.ctx = ctx
	sub.cancel = cancel
	sub.ch = make(chan TelemetryEvent, b.buffer)
	b.mu.Lock()
	b.subs = append(b.subs, sub)
	b.mu.Unlock()

	go b.observe(sub)
	return sub.ch, nil
}

// Close shuts down the bus and closes subscriber channels.
func (b *InMemoryTelemetryBus) Close() {
	b.shutdown.Do(func() {
		b.cancel()
		b.mu.Lock()
		for i, sub := range b.subs {
			if sub != nil {
				sub.close()
			}
			b.subs[i] = nil
		}
		b.subs = nil
		b.mu.Unlock()
	})
}

func (b *InMemoryTelemetryBus) deliver(ctx context.Context, sub *telemetrySubscriber, event TelemetryEvent) error {
	if err := sub.ctx.Err(); err != nil {
		return fmt.Errorf("telemetry subscriber context: %w", err)
	}
	select {
	case <-b.ctx.Done():
		return errs.New("telemetry/publish", errs.CodeUnavailable, errs.WithMessage("telemetry bus closed"))
	case <-ctx.Done():
		return fmt.Errorf("telemetry publish context: %w", ctx.Err())
	case <-sub.ctx.Done():
		return nil
	case sub.ch <- cloneTelemetryEvent(event):
		return nil
	default:
		return errs.New("telemetry/publish", errs.CodeUnavailable, errs.WithMessage("subscriber buffer full"))
	}
}

func (b *InMemoryTelemetryBus) observe(sub *telemetrySubscriber) {
	<-sub.ctx.Done()
	b.mu.Lock()
	for i, candidate := range b.subs {
		if candidate == sub {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			break
		}
	}
	b.mu.Unlock()
	sub.close()
}

func (s *telemetrySubscriber) close() {
	s.once.Do(func() {
		s.cancel()
		close(s.ch)
	})
}

func cloneTelemetryEvent(evt TelemetryEvent) TelemetryEvent {
	clone := evt
	if len(evt.Metadata) > 0 {
		metadataCopy := make(map[string]any, len(evt.Metadata))
		for k, v := range evt.Metadata {
			metadataCopy[k] = v
		}
		clone.Metadata = metadataCopy
	}
	return clone
}
