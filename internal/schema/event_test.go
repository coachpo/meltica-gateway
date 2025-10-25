package schema

import (
	"testing"
)

func TestRouteTypeValidate(t *testing.T) {
	tests := []struct {
		name    string
		route   RouteType
		wantErr bool
	}{
		{
			name:    "valid simple type",
			route:   RouteTypeTicker,
			wantErr: false,
		},
		{
			name:    "valid compound type",
			route:   RouteTypeOrderbookSnapshot,
			wantErr: false,
		},
		{
			name:    "empty type",
			route:   "",
			wantErr: true,
		},
		{
			name:    "lowercase type",
			route:   RouteType("ticker"),
			wantErr: true,
		},
		{
			name:    "type with invalid chars",
			route:   RouteType("TICKER-INVALID"),
			wantErr: true,
		},
		{
			name:    "type with empty segment",
			route:   RouteType("TICKER..INVALID"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.route.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEventRouteMappings(t *testing.T) {
	type routeEventPair struct {
		route RouteType
		event EventType
	}
	pairs := []routeEventPair{
		{route: RouteTypeAccountBalance, event: EventTypeBalanceUpdate},
		{route: RouteTypeOrderbookSnapshot, event: EventTypeBookSnapshot},
		{route: RouteTypeTrade, event: EventTypeTrade},
		{route: RouteTypeTicker, event: EventTypeTicker},
		{route: RouteTypeExecutionReport, event: EventTypeExecReport},
		{route: RouteTypeKlineSummary, event: EventTypeKlineSummary},
	}

	for _, pair := range pairs {
		evt, ok := EventTypeForRoute(pair.route)
		if !ok {
			t.Fatalf("EventTypeForRoute(%s) expected ok", pair.route)
		}
		if evt != pair.event {
			t.Fatalf("EventTypeForRoute(%s) expected %s, got %s", pair.route, pair.event, evt)
		}

		routes := RoutesForEvent(pair.event)
		if len(routes) != 1 || routes[0] != pair.route {
			t.Fatalf("RoutesForEvent(%s) expected [%s], got %v", pair.event, pair.route, routes)
		}

		primary, ok := PrimaryRouteForEvent(pair.event)
		if !ok {
			t.Fatalf("PrimaryRouteForEvent(%s) expected ok", pair.event)
		}
		if primary != pair.route {
			t.Fatalf("PrimaryRouteForEvent(%s) expected %s, got %s", pair.event, pair.route, primary)
		}
	}

	if _, ok := EventTypeForRoute(RouteType("UNKNOWN.ROUTE")); ok {
		t.Fatal("EventTypeForRoute should fail for unknown route")
	}
	if routes := RoutesForEvent(EventType("UnknownEvent")); routes != nil {
		t.Fatalf("RoutesForEvent unknown should be nil, got %v", routes)
	}
	if _, ok := PrimaryRouteForEvent(EventType("UnknownEvent")); ok {
		t.Fatal("PrimaryRouteForEvent unknown expected !ok")
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
			name:    "valid perp instrument",
			symbol:  "BTC-USD-PERP",
			wantErr: false,
		},
		{
			name:    "valid futures instrument",
			symbol:  "BTC-USD-20251227",
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
	key := BuildEventKey("BTC-USD", RouteTypeTicker, 123)

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
