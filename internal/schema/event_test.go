package schema

import (
	"testing"
)

func TestCanonicalTypeValidate(t *testing.T) {
	tests := []struct {
		name    string
		ct      CanonicalType
		wantErr bool
	}{
		{
			name:    "valid simple type",
			ct:      "TICKER",
			wantErr: false,
		},
		{
			name:    "valid compound type",
			ct:      "ORDERBOOK.SNAPSHOT",
			wantErr: false,
		},
		{
			name:    "empty type",
			ct:      "",
			wantErr: true,
		},
		{
			name:    "lowercase type",
			ct:      "ticker",
			wantErr: true,
		},
		{
			name:    "type with invalid chars",
			ct:      "TICKER-INVALID",
			wantErr: true,
		},
		{
			name:    "type with empty segment",
			ct:      "TICKER..INVALID",
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ct.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateInstrument(t *testing.T) {
	tests := []struct {
		name    string
		symbol  string
		wantErr bool
	}{
		{
			name:    "valid instrument",
			symbol:  "BTC-USD",
			wantErr: false,
		},
		{
			name:    "empty instrument",
			symbol:  "",
			wantErr: true,
		},
		{
			name:    "no dash",
			symbol:  "BTCUSD",
			wantErr: true,
		},
		{
			name:    "too many dashes",
			symbol:  "BTC-USD-PERP",
			wantErr: true,
		},
		{
			name:    "empty base",
			symbol:  "-USD",
			wantErr: true,
		},
		{
			name:    "empty quote",
			symbol:  "BTC-",
			wantErr: true,
		},
		{
			name:    "lowercase",
			symbol:  "btc-usd",
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateInstrument(tt.symbol)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateInstrument() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildEventKey(t *testing.T) {
	key := BuildEventKey("BTC-USD", "TICKER", 123)
	
	if key == "" {
		t.Error("expected non-empty key")
	}
	
	// Should contain instrument, type, and seq
	expected := "BTC-USD:TICKER:123"
	if key != expected {
		t.Errorf("expected key %s, got %s", expected, key)
	}
}

func TestRawInstanceClone(t *testing.T) {
	original := RawInstance{
		"key1": "value1",
		"key2": 42,
	}
	
	clone := original.Clone()
	
	if len(clone) != len(original) {
		t.Error("clone should have same length")
	}
	
	// Modify clone
	clone["key1"] = "modified"
	
	// Original should be unchanged
	if original["key1"] != "value1" {
		t.Error("original should not be modified")
	}
}

func TestEventReset(t *testing.T) {
	ev := &Event{
		EventID:        "test-event",
		RoutingVersion: 5,
		Provider:       "binance",
		Symbol:         "BTC-USD",
		Type:           EventTypeTrade,
		SeqProvider:    100,
		Payload:        map[string]interface{}{"price": "50000"},
		TraceID:        "trace-123",
	}
	
	ev.Reset()
	
	if ev.EventID != "" {
		t.Error("expected empty EventID")
	}
	if ev.RoutingVersion != 0 {
		t.Error("expected zero RoutingVersion")
	}
	if ev.Provider != "" {
		t.Error("expected empty Provider")
	}
	if ev.Symbol != "" {
		t.Error("expected empty Symbol")
	}
	if ev.Type != "" {
		t.Error("expected empty Type")
	}
	if ev.SeqProvider != 0 {
		t.Error("expected zero SeqProvider")
	}
	if ev.Payload != nil {
		t.Error("expected nil Payload")
	}
	if ev.TraceID != "" {
		t.Error("expected empty TraceID")
	}
}

func TestEventSetReturned(t *testing.T) {
	ev := &Event{}
	
	if ev.IsReturned() {
		t.Error("expected new event to not be returned")
	}
	
	ev.SetReturned(true)
	
	if !ev.IsReturned() {
		t.Error("expected event to be marked as returned")
	}
	
	ev.SetReturned(false)
	
	if ev.IsReturned() {
		t.Error("expected event to not be returned")
	}
}

func TestEventTypeCoalescable(t *testing.T) {
	tests := []struct {
		name string
		et   EventType
		want bool
	}{
		{"ticker is coalescable", EventTypeTicker, true},
		{"book update is coalescable", EventTypeBookUpdate, true},
		{"kline is coalescable", EventTypeKlineSummary, true},
		{"book snapshot is not coalescable", EventTypeBookSnapshot, false},
		{"trade is not coalescable", EventTypeTrade, false},
		{"exec report is not coalescable", EventTypeExecReport, false},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.et.Coalescable(); got != tt.want {
				t.Errorf("Coalescable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestControlAckPayload(t *testing.T) {
	payload := ControlAckPayload{
		MessageID:      "msg-123",
		ConsumerID:     "consumer-1",
		CommandType:    "subscribe",
		Success:        true,
		RoutingVersion: 5,
	}
	
	if payload.MessageID != "msg-123" {
		t.Error("MessageID not set correctly")
	}
	if !payload.Success {
		t.Error("Success should be true")
	}
}

func TestBookSnapshotPayload(t *testing.T) {
	payload := BookSnapshotPayload{
		Bids: []PriceLevel{
			{Price: "50000", Quantity: "1.5"},
		},
		Asks: []PriceLevel{
			{Price: "50100", Quantity: "2.0"},
		},
		Checksum: "abc123",
	}
	
	if len(payload.Bids) != 1 {
		t.Error("expected 1 bid level")
	}
	if len(payload.Asks) != 1 {
		t.Error("expected 1 ask level")
	}
	if payload.Checksum != "abc123" {
		t.Error("checksum not set correctly")
	}
}

func TestTradePayload(t *testing.T) {
	payload := TradePayload{
		TradeID:  "trade-123",
		Side:     TradeSideBuy,
		Price:    "50000",
		Quantity: "1.5",
	}
	
	if payload.Side != TradeSideBuy {
		t.Error("expected buy side")
	}
	if payload.Price != "50000" {
		t.Error("price not set correctly")
	}
}

func TestExecReportPayload(t *testing.T) {
	reason := "insufficient balance"
	payload := ExecReportPayload{
		ClientOrderID:   "client-123",
		ExchangeOrderID: "exchange-456",
		State:           ExecReportStateREJECTED,
		Side:            TradeSideBuy,
		OrderType:       OrderTypeLimit,
		RejectReason:    &reason,
	}
	
	if payload.State != ExecReportStateREJECTED {
		t.Error("state should be REJECTED")
	}
	if payload.RejectReason == nil || *payload.RejectReason != reason {
		t.Error("reject reason not set correctly")
	}
}
