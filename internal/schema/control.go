// Package schema defines control-plane message structures.
package schema

import (
	"fmt"
	"time"

	json "github.com/goccy/go-json"
)

// ControlMessageType enumerates supported control commands.
type ControlMessageType string

const (
	// ControlMessageSubscribe requests subscription activation.
	ControlMessageSubscribe ControlMessageType = "Subscribe"
	// ControlMessageUnsubscribe requests subscription removal.
	ControlMessageUnsubscribe ControlMessageType = "Unsubscribe"
	// ControlMessageMergedSubscribe requests merged subscription activation.
	ControlMessageMergedSubscribe ControlMessageType = "MergedSubscribe"
	// ControlMessageSetTradingMode requests trading mode updates.
	ControlMessageSetTradingMode ControlMessageType = "SetTradingMode"
)

// ControlMessage is exchanged over the control bus to mutate routing or trading state.
type ControlMessage struct {
	MessageID  string             `json:"message_id"`
	ConsumerID string             `json:"consumer_id"`
	Type       ControlMessageType `json:"type"`
	Payload    json.RawMessage    `json:"payload"`
	Timestamp  time.Time          `json:"timestamp"`
}

// DecodePayload unmarshals the payload into the provided destination.
func (m ControlMessage) DecodePayload(dest any) error {
	if len(m.Payload) == 0 {
		return fmt.Errorf("control message payload empty")
	}
	if dest == nil {
		return fmt.Errorf("control message payload destination nil")
	}
	if err := json.Unmarshal(m.Payload, dest); err != nil {
		return fmt.Errorf("control message payload decode: %w", err)
	}
	return nil
}

// SubscribePayload configures direct provider subscriptions.
type SubscribePayload struct {
	Symbol     string   `json:"symbol"`
	Providers  []string `json:"providers"`
	EventTypes []string `json:"event_types"`
}

// MergedSubscribePayload configures merged multi-provider subscriptions.
type MergedSubscribePayload struct {
	Symbol      string      `json:"symbol"`
	Providers   []string    `json:"providers"`
	EventTypes  []string    `json:"event_types"`
	MergeConfig MergeConfig `json:"merge_config"`
}

// MergeConfig defines windowed merge behaviour for orchestrator subscriptions.
type MergeConfig struct {
	WindowDuration time.Duration `json:"window_duration"`
	MaxEvents      int           `json:"max_events"`
	PartialPolicy  string        `json:"partial_policy"`
}

// TradingModePayload flips the trading switch for a consumer.
type TradingModePayload struct {
	Enabled bool `json:"enabled"`
}

// ControlAcknowledgement is returned to acknowledge control commands.
type ControlAcknowledgement struct {
	MessageID      string    `json:"message_id"`
	ConsumerID     string    `json:"consumer_id"`
	Success        bool      `json:"success"`
	RoutingVersion int       `json:"routing_version"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}
