package controlbus

import (
	"context"
	"testing"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

func TestNewMemoryBus(t *testing.T) {
	cfg := MemoryConfig{BufferSize: 10}
	bus := NewMemoryBus(cfg)
	
	if bus == nil {
		t.Fatal("expected non-nil bus")
	}
	
	bus.Close()
}

func TestMemoryBusSendNoConsumers(t *testing.T) {
	bus := NewMemoryBus(MemoryConfig{BufferSize: 10})
	defer bus.Close()
	
	ctx := context.Background()
	cmd := schema.ControlMessage{
		Type:      "subscribe",
		MessageID: "msg-1",
	}
	
	_, err := bus.Send(ctx, cmd)
	if err == nil {
		t.Error("expected error when no consumers available")
	}
}

func TestMemoryBusSendEmptyType(t *testing.T) {
	bus := NewMemoryBus(MemoryConfig{BufferSize: 10})
	defer bus.Close()
	
	ctx := context.Background()
	cmd := schema.ControlMessage{
		Type: "", // Empty
	}
	
	_, err := bus.Send(ctx, cmd)
	if err == nil {
		t.Error("expected error for empty command type")
	}
}

func TestMemoryBusSendAndReceive(t *testing.T) {
	bus := NewMemoryBus(MemoryConfig{BufferSize: 10})
	defer bus.Close()
	
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	// Start consumer
	msgCh, err := bus.Consume(ctx)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	
	// Consumer goroutine
	go func() {
		select {
		case msg := <-msgCh:
			if msg.Command.Type != "subscribe" {
				t.Errorf("expected subscribe command, got %s", msg.Command.Type)
			}
			// Send acknowledgement
			msg.Reply <- schema.ControlAcknowledgement{
				MessageID: msg.Command.MessageID,
				Success:   true,
			}
		case <-time.After(1 * time.Second):
			t.Error("consumer timeout waiting for message")
		}
	}()
	
	// Send command
	cmd := schema.ControlMessage{
		Type:      "subscribe",
		MessageID: "msg-1",
	}
	
	ack, err := bus.Send(ctx, cmd)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	
	if !ack.Success {
		t.Error("expected successful acknowledgement")
	}
	if ack.MessageID != "msg-1" {
		t.Errorf("expected ack MessageID msg-1, got %s", ack.MessageID)
	}
}

func TestMemoryBusConsumeMultiple(t *testing.T) {
	bus := NewMemoryBus(MemoryConfig{BufferSize: 10})
	defer bus.Close()
	
	ctx := context.Background()
	
	// Start first consumer
	ch1, err1 := bus.Consume(ctx)
	if err1 != nil {
		t.Fatalf("Consume 1 error = %v", err1)
	}
	
	// Start second consumer
	ch2, err2 := bus.Consume(ctx)
	if err2 != nil {
		t.Fatalf("Consume 2 error = %v", err2)
	}
	
	if ch1 == nil || ch2 == nil {
		t.Error("expected non-nil channels")
	}
}

func TestMemoryBusClose(t *testing.T) {
	bus := NewMemoryBus(MemoryConfig{BufferSize: 10})
	
	ctx := context.Background()
	msgCh, err := bus.Consume(ctx)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	
	bus.Close()
	
	// Channel should be closed
	select {
	case _, ok := <-msgCh:
		if ok {
			t.Error("expected channel to be closed after bus close")
		}
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}

func TestMemoryConfigNormalize(t *testing.T) {
	cfg := MemoryConfig{BufferSize: 0}
	normalized := cfg.normalize()
	
	if normalized.BufferSize <= 0 {
		t.Error("expected positive buffer size after normalization")
	}
}

func TestMemoryBusSendContextCanceled(t *testing.T) {
	bus := NewMemoryBus(MemoryConfig{BufferSize: 1})
	defer bus.Close()
	
	ctx, cancel := context.WithCancel(context.Background())
	
	// Start consumer but don't process messages
	_, err := bus.Consume(ctx)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	
	// Cancel context immediately
	cancel()
	
	// Try to send
	cmd := schema.ControlMessage{
		Type:      "test",
		MessageID: "msg-1",
	}
	
	_, err = bus.Send(ctx, cmd)
	if err == nil {
		t.Error("expected error when context is canceled")
	}
}
