package eventbus

import (
	"context"
	"errors"
	"strings"
	"testing"

	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/domain/outboxstore"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/pool"
)

func TestNewDurableBusReturnsInnerWhenStoreNil(t *testing.T) {
	inner := &stubBus{}
	wrapped := NewDurableBus(inner, nil)
	if wrapped != inner {
		t.Fatalf("expected original bus when store nil")
	}
}

func TestDurableBusPublishPersistsAndMarksDelivered(t *testing.T) {
	inner := &stubBus{}
	store := &fakeOutboxStore{}
	bus := NewDurableBus(inner, store, WithReplayDisabled())
	if bus == nil {
		t.Fatalf("expected durable bus instance")
	}
	event := &schema.Event{
		EventID:     "evt-1",
		Provider:    "binance",
		Symbol:      "BTCUSDT",
		Type:        schema.EventTypeTrade,
		SeqProvider: 42,
	}
	if err := bus.Publish(context.Background(), event); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if len(inner.published) != 1 {
		t.Fatalf("expected publish delegation, got %d", len(inner.published))
	}
	if len(store.delivered) != 1 {
		t.Fatalf("expected delivered marker, got %d", len(store.delivered))
	}
	if len(store.failed) != 0 {
		t.Fatalf("unexpected failures: %v", store.failed)
	}
	bus.Close()
}

func TestDurableBusPublishRecordsFailure(t *testing.T) {
	pubErr := errors.New("publish failed")
	inner := &stubBus{publishErr: pubErr}
	store := &fakeOutboxStore{}
	bus := NewDurableBus(inner, store, WithReplayDisabled())
	event := &schema.Event{
		EventID:     "evt-2",
		Provider:    "coinbase",
		Symbol:      "ETHUSD",
		Type:        schema.EventTypeTrade,
		SeqProvider: 7,
	}
	err := bus.Publish(context.Background(), event)
	if !errors.Is(err, pubErr) {
		t.Fatalf("expected publish error, got %v", err)
	}
	if len(store.failed) != 1 {
		t.Fatalf("expected failure recorded, got %d", len(store.failed))
	}
	if len(store.delivered) != 0 {
		t.Fatalf("expected no delivered rows, got %d", len(store.delivered))
	}
	bus.Close()
}

func TestDurableBusReplayUsesEventPool(t *testing.T) {
	poolMgr := pool.NewPoolManager()
	err := poolMgr.RegisterPool("Event", 2, 2, func() any { return new(schema.Event) })
	if err != nil {
		t.Fatalf("register event pool: %v", err)
	}
	inner := NewMemoryBus(MemoryConfig{BufferSize: 4, FanoutWorkers: 1, Pools: poolMgr})
	store := &fakeOutboxStore{}
	wrapped := NewDurableBus(inner, store, WithReplayDisabled())
	durable, ok := wrapped.(*DurableBus)
	if !ok {
		t.Fatalf("expected durable bus instance")
	}
	durable.replayCtx = context.Background()
	evt := &schema.Event{
		EventID:  "evt-3",
		Provider: "binance",
		Symbol:   "BTCUSDT",
		Type:     schema.EventTypeTrade,
	}
	payload, err := eventToJSON(evt)
	if err != nil {
		t.Fatalf("eventToMap failed: %v", err)
	}
	store.pending = append(store.pending, outboxstore.EventRecord{ID: 1, EventType: string(evt.Type), Payload: payload})

	durable.replayPendingEvents()

	if len(store.delivered) != 1 {
		t.Fatalf("expected delivered record, got %d", len(store.delivered))
	}
	inner.Close()
}

type stubBus struct {
	published  []*schema.Event
	publishErr error
}

func (s *stubBus) Publish(_ context.Context, evt *schema.Event) error {
	if s.publishErr != nil {
		return s.publishErr
	}
	s.published = append(s.published, evt)
	return nil
}

func (*stubBus) Subscribe(context.Context, schema.EventType) (SubscriptionID, <-chan *schema.Event, error) {
	return "stub", make(chan *schema.Event), nil
}

func (*stubBus) Unsubscribe(SubscriptionID) {}

func (s *stubBus) Close() {}

type fakeOutboxStore struct {
	nextID    int64
	enqueued  []outboxstore.Event
	delivered []int64
	failed    []int64
	pending   []outboxstore.EventRecord
}

func (s *fakeOutboxStore) Enqueue(_ context.Context, evt outboxstore.Event) (outboxstore.EventRecord, error) {
	s.nextID++
	s.enqueued = append(s.enqueued, evt)
	payload := json.RawMessage(append([]byte(nil), evt.Payload...))
	record := outboxstore.EventRecord{ID: s.nextID, Payload: payload, EventType: evt.EventType}
	s.pending = append(s.pending, record)
	return record, nil
}

func (s *fakeOutboxStore) ListPending(_ context.Context, _ int) ([]outboxstore.EventRecord, error) {
	if len(s.pending) == 0 {
		return nil, nil
	}
	batch := s.pending
	s.pending = nil
	return batch, nil
}

func (s *fakeOutboxStore) MarkDelivered(_ context.Context, id int64) error {
	s.delivered = append(s.delivered, id)
	return nil
}

func (s *fakeOutboxStore) MarkFailed(_ context.Context, id int64, _ string) error {
	s.failed = append(s.failed, id)
	return nil
}

func (s *fakeOutboxStore) Delete(context.Context, int64) error {
	return nil
}
func TestDurableBusReplayPreservesSequenceAndPayload(t *testing.T) {
	inner := &stubBus{}
	store := &fakeOutboxStore{}
	bus := NewDurableBus(inner, store, WithReplayDisabled())
	durable, ok := bus.(*DurableBus)
	if !ok {
		t.Fatalf("expected durable bus implementation")
	}
	durable.replayCtx = context.Background()

	event := &schema.Event{
		EventID:     "evt-big",
		Provider:    "binance",
		Symbol:      "BTCUSDT",
		Type:        schema.EventTypeTrade,
		SeqProvider: 9007199254740995, // > 2^53 to catch float rounding
		Payload: schema.TradePayload{
			TradeID:  "t1",
			Side:     schema.TradeSideBuy,
			Price:    "123.45",
			Quantity: "1.0",
		},
	}
	raw, err := eventToJSON(event)
	if err != nil {
		t.Fatalf("encode event: %v", err)
	}
	store.pending = []outboxstore.EventRecord{{ID: 42, EventType: string(event.Type), Payload: raw}}

	durable.replayPendingEvents()

	if len(inner.published) != 1 {
		t.Fatalf("expected replayed publish, got %d", len(inner.published))
	}
	replayed := inner.published[0]
	if replayed.SeqProvider != event.SeqProvider {
		t.Fatalf("seq provider mismatch: want %d got %d", event.SeqProvider, replayed.SeqProvider)
	}
	payload, ok := replayed.Payload.(schema.TradePayload)
	if !ok {
		t.Fatalf("expected TradePayload, got %T", replayed.Payload)
	}
	if payload.TradeID != "t1" {
		t.Fatalf("expected payload trade id t1, got %s", payload.TradeID)
	}
}

func TestDurableBusPublishExtensionPayloadRoundTrip(t *testing.T) {
	inner := &stubBus{}
	store := &fakeOutboxStore{}
	bus := NewDurableBus(inner, store, WithReplayDisabled(), WithExtensionPayloadCapBytes(1024))
	ext := &schema.Event{
		EventID:  "ext-1",
		Provider: "test",
		Type:     schema.ExtensionEventType,
		Payload: map[string]any{
			"custom": map[string]any{"value": "alpha"},
		},
	}
	if err := bus.Publish(context.Background(), ext); err != nil {
		t.Fatalf("publish extension event: %v", err)
	}
	if len(store.enqueued) != 1 {
		t.Fatalf("expected enqueued record, got %d", len(store.enqueued))
	}
	if len(inner.published) != 1 {
		t.Fatalf("expected publish delegation, got %d", len(inner.published))
	}
	payload, ok := inner.published[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", inner.published[0].Payload)
	}
	nested, ok := payload["custom"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested custom payload, got %T", payload["custom"])
	}
	if value := nested["value"]; value != "alpha" {
		t.Fatalf("expected nested value alpha, got %v", value)
	}
}

func TestDurableBusPublishExtensionPayloadOverCap(t *testing.T) {
	inner := &stubBus{}
	store := &fakeOutboxStore{}
	bus := NewDurableBus(inner, store, WithReplayDisabled(), WithExtensionPayloadCapBytes(16))
	tooLarge := &schema.Event{
		EventID:  "ext-big",
		Provider: "test",
		Type:     schema.ExtensionEventType,
		Payload: map[string]any{"data": strings.Repeat("x", 128)},
	}
	if err := bus.Publish(context.Background(), tooLarge); err == nil {
		t.Fatal("expected error for extension payload exceeding cap")
	}
	if len(store.enqueued) != 0 {
		t.Fatalf("expected no enqueued records, got %d", len(store.enqueued))
	}
	if len(inner.published) != 0 {
		t.Fatalf("expected no inner publishes, got %d", len(inner.published))
	}
}
