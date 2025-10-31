package okx

import (
	"testing"

	"github.com/coachpo/meltica/internal/domain/schema"
)

func TestBuildInstrument(t *testing.T) {
	record := instrumentRecord{
		InstID:   "btc-usdt",
		InstType: "SPOT",
		BaseCcy:  "btc",
		QuoteCcy: "usdt",
		TickSz:   "0.1",
		LotSz:    "0.0001",
		MinSz:    "0.0001",
		State:    "live",
	}
	inst, meta, err := buildInstrument(record, "OKX")
	if err != nil {
		t.Fatalf("buildInstrument returned error: %v", err)
	}
	if inst.Symbol != "BTC-USDT" {
		t.Fatalf("unexpected symbol: %s", inst.Symbol)
	}
	if inst.Type != schema.InstrumentTypeSpot {
		t.Fatalf("unexpected type: %s", inst.Type)
	}
	if inst.PriceIncrement != "0.1" {
		t.Fatalf("unexpected price increment: %s", inst.PriceIncrement)
	}
	if inst.QuantityIncrement != "0.0001" {
		t.Fatalf("unexpected qty increment: %s", inst.QuantityIncrement)
	}
	if inst.PricePrecision == nil || *inst.PricePrecision != 1 {
		t.Fatalf("expected price precision 1, got %v", inst.PricePrecision)
	}
	if inst.QuantityPrecision == nil || *inst.QuantityPrecision != 4 {
		t.Fatalf("expected quantity precision 4, got %v", inst.QuantityPrecision)
	}
	if meta.canonical != "BTC-USDT" {
		t.Fatalf("unexpected meta canonical: %s", meta.canonical)
	}
	if meta.instID != "btc-usdt" {
		t.Fatalf("unexpected meta instID: %s", meta.instID)
	}
}

func TestPrecisionFromStep(t *testing.T) {
	cases := []struct {
		input    string
		expected int
	}{
		{"1", 0},
		{"0.1", 1},
		{"0.0100", 2},
		{"0.000000", 0},
	}
	for _, tc := range cases {
		actual, ok := precisionFromStep(tc.input)
		if !ok {
			t.Fatalf("precisionFromStep(%q) returned false", tc.input)
		}
		if actual != tc.expected {
			t.Fatalf("precisionFromStep(%q) = %d, want %d", tc.input, actual, tc.expected)
		}
	}
}

func TestConvertDiffLevels(t *testing.T) {
	levels := [][]string{{"100", "1"}, {"", ""}, {"101", ""}}
	converted := convertDiffLevels(levels)
	if len(converted) != 2 {
		t.Fatalf("expected 2 diff levels, got %d", len(converted))
	}
	if converted[0].Price != "100" || converted[0].Quantity != "1" {
		t.Fatalf("unexpected first diff level: %+v", converted[0])
	}
	if converted[1].Price != "101" || converted[1].Quantity != "" {
		t.Fatalf("unexpected second diff level: %+v", converted[1])
	}
}
