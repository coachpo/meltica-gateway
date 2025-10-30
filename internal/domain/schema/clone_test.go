package schema

import (
	"testing"
)

func TestCopyEvent(t *testing.T) {
	original := &Event{
		EventID:  "evt-123",
		Provider: "binance",
		Symbol:   "BTC-USD",
		Type:     "TRADE",
		Payload: TradePayload{
			Price:    "50000.00",
			Quantity: "1.5",
			Side:     TradeSideBuy,
		},
	}

	dst := &Event{}
	CopyEvent(dst, original)

	if dst.Provider != original.Provider {
		t.Error("expected Provider to match")
	}
	if dst.Symbol != original.Symbol {
		t.Error("expected Symbol to match")
	}
	if dst.Type != original.Type {
		t.Error("expected Type to match")
	}
}

func TestCopyEventNil(t *testing.T) {
	src := &Event{Provider: "test"}
	CopyEvent(nil, src) // Should not panic

	dst := &Event{}
	CopyEvent(dst, nil) // Should not panic
}

func TestCopyEventBookSnapshot(t *testing.T) {
	original := &Event{
		EventID:  "evt-snap-1",
		Provider: "binance",
		Symbol:   "BTC-USD",
		Type:     "BOOK_SNAPSHOT",
		Payload: BookSnapshotPayload{
			Bids: []PriceLevel{
				{Price: "49900.00", Quantity: "3.0"},
			},
			Asks: []PriceLevel{
				{Price: "50200.00", Quantity: "4.0"},
			},
		},
	}

	dst := &Event{}
	CopyEvent(dst, original)

	payload, ok := dst.Payload.(BookSnapshotPayload)
	if !ok {
		t.Fatal("expected BookSnapshotPayload")
	}
	if len(payload.Bids) != 1 {
		t.Error("expected 1 bid level")
	}
	if len(payload.Asks) != 1 {
		t.Error("expected 1 ask level")
	}
}

func TestCloneMapStringAny(t *testing.T) {
	original := map[string]any{
		"key1": "value1",
		"key2": 42,
		"key3": true,
	}

	copy := cloneMapStringAny(original)

	if copy == nil {
		t.Fatal("expected non-nil copy")
	}
	if len(copy) != len(original) {
		t.Error("expected same length")
	}
	if copy["key1"] != "value1" {
		t.Error("expected key1 value to match")
	}
	if copy["key2"] != 42 {
		t.Error("expected key2 value to match")
	}
}

func TestClonePriceLevels(t *testing.T) {
	original := []PriceLevel{
		{Price: "100.00", Quantity: "1.0"},
		{Price: "101.00", Quantity: "2.0"},
	}

	copy := clonePriceLevels(original)

	if copy == nil {
		t.Fatal("expected non-nil copy")
	}
	if len(copy) != len(original) {
		t.Error("expected same length")
	}
	if copy[0].Price != "100.00" {
		t.Error("expected first price to match")
	}
	if copy[1].Quantity != "2.0" {
		t.Error("expected second quantity to match")
	}
}
