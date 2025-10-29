package eventbus

import (
	"context"
	"testing"
	"time"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

// setupTestBus creates a bus with properly initialized pool manager for testing
func setupTestBus(t *testing.T) (Bus, *pool.PoolManager) {
	t.Helper()

	poolMgr := pool.NewPoolManager()
	err := poolMgr.RegisterPool("Event", 100, 0, func() interface{} {
		return new(schema.Event)
	})
	if err != nil {
		t.Fatalf("failed to register pool: %v", err)
	}

	bus := NewMemoryBus(MemoryConfig{
		BufferSize:    10,
		FanoutWorkers: 2,
		Pools:         poolMgr,
	})

	return bus, poolMgr
}

func TestNewMemoryBus(t *testing.T) {
	cfg := MemoryConfig{
		BufferSize:    10,
		FanoutWorkers: 2,
	}

	bus := NewMemoryBus(cfg)

	if bus == nil {
		t.Fatal("expected non-nil bus")
	}

	bus.Close()
}

func TestMemoryBusPublishNoSubscribers(t *testing.T) {
	bus := NewMemoryBus(MemoryConfig{BufferSize: 10})
	defer bus.Close()

	ctx := context.Background()
	evt := &schema.Event{
		EventID:  "test-1",
		Provider: "test",
		Type:     schema.EventTypeTrade,
		Symbol:   "BTC-USD",
	}

	// Should not error when no subscribers
	err := bus.Publish(ctx, evt)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMemoryBusPublishNilEvent(t *testing.T) {
	bus := NewMemoryBus(MemoryConfig{BufferSize: 10})
	defer bus.Close()

	ctx := context.Background()
	err := bus.Publish(ctx, nil)

	if err != nil {
		t.Errorf("expected no error for nil event, got %v", err)
	}
}

func TestMemoryBusPublishEmptyType(t *testing.T) {
	bus := NewMemoryBus(MemoryConfig{BufferSize: 10})
	defer bus.Close()

	ctx := context.Background()
	evt := &schema.Event{
		EventID: "test-1",
		Type:    "", // Empty type
	}

	err := bus.Publish(ctx, evt)
	if err == nil {
		t.Error("expected error for empty event type")
	}
}

func TestMemoryBusSubscribeAndPublish(t *testing.T) {
	bus, poolMgr := setupTestBus(t)
	defer bus.Close()
	defer poolMgr.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Subscribe
	subID, eventsCh, err := bus.Subscribe(ctx, schema.EventTypeTrade)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer bus.Unsubscribe(subID)

	// Publish - borrow event from pool
	testEvent, err := poolMgr.BorrowEventInst(ctx)
	if err != nil {
		t.Fatalf("BorrowEventInst() error = %v", err)
	}
	expectedEventID := "test-1"
	testEvent.EventID = expectedEventID
	testEvent.Provider = "binance"
	testEvent.Type = schema.EventTypeTrade
	testEvent.Symbol = "BTC-USD"

	err = bus.Publish(ctx, testEvent)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	// Receive
	select {
	case received := <-eventsCh:
		if received == nil {
			t.Fatal("received nil event")
		}
		if received.EventID != expectedEventID {
			t.Errorf("expected EventID %s, got %s", expectedEventID, received.EventID)
		}
		// Recycle the received event
		poolMgr.ReturnEventInst(received)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestMemoryBusSubscribeEmptyType(t *testing.T) {
	bus := NewMemoryBus(MemoryConfig{BufferSize: 10})
	defer bus.Close()

	ctx := context.Background()
	_, _, err := bus.Subscribe(ctx, "")

	if err == nil {
		t.Error("expected error for empty event type")
	}
}

func TestMemoryBusUnsubscribe(t *testing.T) {
	bus := NewMemoryBus(MemoryConfig{BufferSize: 10})
	defer bus.Close()

	ctx := context.Background()
	subID, eventsCh, err := bus.Subscribe(ctx, schema.EventTypeTrade)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	bus.Unsubscribe(subID)

	// Channel should be closed
	select {
	case _, ok := <-eventsCh:
		if ok {
			t.Error("expected channel to be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for channel close")
	}
}

func TestMemoryBusClose(t *testing.T) {
	bus := NewMemoryBus(MemoryConfig{BufferSize: 10})

	ctx := context.Background()
	_, eventsCh, err := bus.Subscribe(ctx, schema.EventTypeTrade)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	bus.Close()

	// Channel should be closed
	select {
	case _, ok := <-eventsCh:
		if ok {
			t.Error("expected channel to be closed after bus close")
		}
	case <-time.After(100 * time.Millisecond):
		// Expected - channel closed
	}
}

func TestMemoryBusMultipleSubscribers(t *testing.T) {
	bus, poolMgr := setupTestBus(t)
	defer bus.Close()
	defer poolMgr.Shutdown(context.Background())

	ctx := context.Background()

	// Subscribe twice
	sub1, ch1, err1 := bus.Subscribe(ctx, schema.EventTypeTrade)
	if err1 != nil {
		t.Fatalf("Subscribe 1 error = %v", err1)
	}
	defer bus.Unsubscribe(sub1)

	sub2, ch2, err2 := bus.Subscribe(ctx, schema.EventTypeTrade)
	if err2 != nil {
		t.Fatalf("Subscribe 2 error = %v", err2)
	}
	defer bus.Unsubscribe(sub2)

	// Publish - borrow event from pool
	testEvent, err := poolMgr.BorrowEventInst(ctx)
	if err != nil {
		t.Fatalf("BorrowEventInst() error = %v", err)
	}
	expectedEventID := "test-multi"
	testEvent.EventID = expectedEventID
	testEvent.Provider = "binance"
	testEvent.Type = schema.EventTypeTrade
	testEvent.Symbol = "ETH-USD"

	err = bus.Publish(ctx, testEvent)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	// Both should receive
	timeout := time.After(1 * time.Second)
	received1 := false
	received2 := false

	for !received1 || !received2 {
		select {
		case evt := <-ch1:
			if evt != nil && evt.EventID == expectedEventID {
				received1 = true
				poolMgr.ReturnEventInst(evt)
			}
		case evt := <-ch2:
			if evt != nil && evt.EventID == expectedEventID {
				received2 = true
				poolMgr.ReturnEventInst(evt)
			}
		case <-timeout:
			if !received1 {
				t.Error("subscriber 1 did not receive event")
			}
			if !received2 {
				t.Error("subscriber 2 did not receive event")
			}
			return
		}
	}
}

func TestMemoryConfigNormalize(t *testing.T) {
	cfg := MemoryConfig{
		BufferSize:    0, // Should be normalized
		FanoutWorkers: 0, // Should be normalized
	}

	normalized := cfg.normalize()

	if normalized.BufferSize <= 0 {
		t.Error("expected positive buffer size after normalization")
	}
	if normalized.FanoutWorkers <= 0 {
		t.Error("expected positive fanout workers after normalization")
	}
}
