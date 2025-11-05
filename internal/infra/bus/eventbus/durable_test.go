package eventbus

import (
	"context"
	"errors"
	"testing"

	"github.com/coachpo/meltica/internal/domain/outboxstore"
	"github.com/coachpo/meltica/internal/domain/schema"
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
}

func (s *fakeOutboxStore) Enqueue(_ context.Context, evt outboxstore.Event) (outboxstore.EventRecord, error) {
	s.nextID++
	s.enqueued = append(s.enqueued, evt)
	return outboxstore.EventRecord{ID: s.nextID, Payload: evt.Payload}, nil
}

func (s *fakeOutboxStore) ListPending(_ context.Context, _ int) ([]outboxstore.EventRecord, error) {
	return nil, nil
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
