package schema

import (
	"testing"
	"time"
)

func TestControlMessageDecodePayload(t *testing.T) {
	msg := ControlMessage{
		MessageID:  "msg-123",
		ConsumerID: "consumer-1",
		Type:       ControlMessageSubscribe,
		Payload:    []byte(`{"type":"TICKER"}`),
		Timestamp:  time.Now(),
	}

	var dest Subscribe
	err := msg.DecodePayload(&dest)
	if err != nil {
		t.Fatalf("DecodePayload failed: %v", err)
	}

	if dest.Type != RouteTypeTicker {
		t.Errorf("expected type %s, got %s", RouteTypeTicker, dest.Type)
	}
}

func TestControlMessageDecodePayloadEmpty(t *testing.T) {
	msg := ControlMessage{
		MessageID:  "msg-123",
		ConsumerID: "consumer-1",
		Type:       ControlMessageSubscribe,
		Payload:    nil,
		Timestamp:  time.Now(),
	}

	var dest Subscribe
	err := msg.DecodePayload(&dest)
	if err == nil {
		t.Error("expected error for empty payload")
	}
}

func TestControlMessageDecodePayloadNilDest(t *testing.T) {
	msg := ControlMessage{
		MessageID:  "msg-123",
		ConsumerID: "consumer-1",
		Type:       ControlMessageSubscribe,
		Payload:    []byte(`{}`),
		Timestamp:  time.Now(),
	}

	err := msg.DecodePayload(nil)
	if err == nil {
		t.Error("expected error for nil destination")
	}
}

func TestSubscribe(t *testing.T) {
	sub := Subscribe{
		Type: RouteTypeTicker,
		Filters: map[string]any{
			"symbol": "BTC-USD",
		},
		RequestID: "req-123",
	}

	if sub.Type == "" {
		t.Error("expected non-empty Type")
	}
	if sub.RequestID == "" {
		t.Error("expected non-empty RequestID")
	}

	// Test type validation through RouteType
	err := sub.Type.Validate()
	if err != nil {
		t.Errorf("Type.Validate failed: %v", err)
	}
}

func TestSubscribeValidateEmpty(t *testing.T) {
	sub := Subscribe{}

	// Empty type should fail validation
	err := sub.Type.Validate()
	if err == nil {
		t.Error("expected error for empty type")
	}
}

func TestUnsubscribe(t *testing.T) {
	unsub := Unsubscribe{
		Type:      RouteTypeTicker,
		RequestID: "req-123",
	}

	if unsub.Type == "" {
		t.Error("expected non-empty Type")
	}

	// Test type validation through RouteType
	err := unsub.Type.Validate()
	if err != nil {
		t.Errorf("Type.Validate failed: %v", err)
	}
}

func TestUnsubscribeValidateEmpty(t *testing.T) {
	unsub := Unsubscribe{}

	// Empty type should fail validation
	err := unsub.Type.Validate()
	if err == nil {
		t.Error("expected error for empty type")
	}
}

func TestTradingModePayload(t *testing.T) {
	payload := TradingModePayload{
		Enabled: true,
	}

	if !payload.Enabled {
		t.Error("expected enabled to be true")
	}

	payload.Enabled = false
	if payload.Enabled {
		t.Error("expected enabled to be false")
	}
}

func TestControlAcknowledgement(t *testing.T) {
	ack := ControlAcknowledgement{
		MessageID:      "msg-123",
		ConsumerID:     "consumer-1",
		Success:        true,
		RoutingVersion: 5,
		Timestamp:      time.Now(),
	}

	if ack.MessageID == "" {
		t.Error("expected non-empty MessageID")
	}
	if !ack.Success {
		t.Error("expected success to be true")
	}
	if ack.RoutingVersion != 5 {
		t.Errorf("expected routing version 5, got %d", ack.RoutingVersion)
	}
}
