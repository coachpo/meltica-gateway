package dispatcher

import (
	"context"
	"testing"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

func TestNewIngestor(t *testing.T) {
	table := NewTable()
	ingestor := NewIngestor(table)
	
	if ingestor == nil {
		t.Fatal("expected non-nil ingestor")
	}
	
	if ingestor.table != table {
		t.Error("expected ingestor to use provided table")
	}
	
	if ingestor.seq == nil {
		t.Error("expected initialized sequence map")
	}
}

func TestIngestorRun(t *testing.T) {
	table := NewTable()
	route := Route{
		Type: "TICKER",
		Filters: []FilterRule{
			{Field: "instrument", Op: "eq", Value: "BTC-USD"},
		},
	}
	_ = table.Upsert(route)
	
	ingestor := NewIngestor(table)
	
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	rawStream := make(chan schema.RawInstance, 1)
	events, errs := ingestor.Run(ctx, rawStream)
	
	// Send a raw event
	rawStream <- schema.RawInstance{
		"canonicalType": "TICKER",
		"instrument":    "BTC-USD",
		"source":        "binance",
		"ts":            time.Now().UnixMilli(),
		"payload":       map[string]any{"price": "50000"},
	}
	close(rawStream)
	
	// Receive canonical event
	select {
	case evt := <-events:
		if evt.Type != "TICKER" {
			t.Errorf("expected type TICKER, got %s", evt.Type)
		}
		if evt.Instrument != "BTC-USD" {
			t.Errorf("expected instrument BTC-USD, got %s", evt.Instrument)
		}
	case err := <-errs:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}

func TestIngestorMissingCanonicalType(t *testing.T) {
	table := NewTable()
	ingestor := NewIngestor(table)
	
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	rawStream := make(chan schema.RawInstance, 1)
	_, errs := ingestor.Run(ctx, rawStream)
	
	// Send event without canonicalType
	rawStream <- schema.RawInstance{
		"instrument": "BTC-USD",
	}
	close(rawStream)
	
	// Should receive error
	select {
	case err := <-errs:
		if err == nil {
			t.Error("expected error for missing canonicalType")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for error")
	}
}

func TestIngestorRouteNotFound(t *testing.T) {
	table := NewTable()
	ingestor := NewIngestor(table)
	
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	rawStream := make(chan schema.RawInstance, 1)
	_, errs := ingestor.Run(ctx, rawStream)
	
	// Send event with unknown type
	rawStream <- schema.RawInstance{
		"canonicalType": "UNKNOWN",
		"instrument":    "BTC-USD",
	}
	close(rawStream)
	
	// Should receive error
	select {
	case err := <-errs:
		if err == nil {
			t.Error("expected error for unknown route")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for error")
	}
}

func TestIngestorSequenceGeneration(t *testing.T) {
	table := NewTable()
	route := Route{
		Type: "TICKER",
	}
	_ = table.Upsert(route)
	
	ingestor := NewIngestor(table)
	
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	rawStream := make(chan schema.RawInstance, 3)
	events, _ := ingestor.Run(ctx, rawStream)
	
	// Send multiple events for same instrument
	for i := 0; i < 3; i++ {
		rawStream <- schema.RawInstance{
			"canonicalType": "TICKER",
			"instrument":    "BTC-USD",
			"source":        "binance",
			"ts":            time.Now().UnixMilli(),
		}
	}
	close(rawStream)
	
	// Check sequences increment
	var lastSeq uint64
	for i := 0; i < 3; i++ {
		select {
		case evt := <-events:
			if evt.Seq <= lastSeq && lastSeq != 0 {
				t.Errorf("expected sequence to increment, got %d after %d", evt.Seq, lastSeq)
			}
			lastSeq = evt.Seq
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("timeout waiting for event %d", i+1)
		}
	}
	
	if lastSeq != 3 {
		t.Errorf("expected final sequence to be 3, got %d", lastSeq)
	}
}

func TestIngestorDifferentInstruments(t *testing.T) {
	table := NewTable()
	route := Route{
		Type: "TICKER",
	}
	_ = table.Upsert(route)
	
	ingestor := NewIngestor(table)
	
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	rawStream := make(chan schema.RawInstance, 2)
	events, _ := ingestor.Run(ctx, rawStream)
	
	// Send events for different instruments
	rawStream <- schema.RawInstance{
		"canonicalType": "TICKER",
		"instrument":    "BTC-USD",
		"source":        "binance",
		"ts":            time.Now().UnixMilli(),
	}
	rawStream <- schema.RawInstance{
		"canonicalType": "TICKER",
		"instrument":    "ETH-USD",
		"source":        "binance",
		"ts":            time.Now().UnixMilli(),
	}
	close(rawStream)
	
	// Both should start at sequence 1
	seqMap := make(map[string]uint64)
	for i := 0; i < 2; i++ {
		select {
		case evt := <-events:
			seqMap[evt.Instrument] = evt.Seq
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("timeout waiting for event %d", i+1)
		}
	}
	
	if seqMap["BTC-USD"] != 1 {
		t.Errorf("expected BTC-USD sequence 1, got %d", seqMap["BTC-USD"])
	}
	if seqMap["ETH-USD"] != 1 {
		t.Errorf("expected ETH-USD sequence 1, got %d", seqMap["ETH-USD"])
	}
}

func TestToCanonicalType(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  schema.CanonicalType
		ok    bool
	}{
		{
			name:  "string value",
			value: "TICKER",
			want:  "TICKER",
			ok:    true,
		},
		{
			name:  "lowercase string",
			value: "ticker",
			want:  "TICKER",
			ok:    true,
		},
		{
			name:  "string with whitespace",
			value: "  ticker  ",
			want:  "TICKER",
			ok:    true,
		},
		{
			name:  "canonical type",
			value: schema.CanonicalType("TICKER"),
			want:  "TICKER",
			ok:    true,
		},
		{
			name:  "invalid type",
			value: 123,
			want:  "",
			ok:    false,
		},
		{
			name:  "nil value",
			value: nil,
			want:  "",
			ok:    false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toCanonicalType(tt.value)
			if ok != tt.ok {
				t.Errorf("toCanonicalType() ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("toCanonicalType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractTimestamp(t *testing.T) {
	now := time.Now().UTC()
	
	tests := []struct {
		name  string
		value any
	}{
		{
			name:  "time.Time",
			value: now,
		},
		{
			name:  "int64 milliseconds",
			value: now.UnixMilli(),
		},
		{
			name:  "float64 milliseconds",
			value: float64(now.UnixMilli()),
		},
		{
			name:  "invalid type",
			value: "invalid",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTimestamp(tt.value)
			if result.IsZero() && tt.name != "invalid type" {
				t.Error("expected non-zero timestamp")
			}
		})
	}
}

func TestComputeLatency(t *testing.T) {
	now := time.Now().UTC()
	earlier := now.Add(-100 * time.Millisecond)
	
	tests := []struct {
		name      string
		raw       any
		event     time.Time
		wantZero  bool
	}{
		{
			name:     "valid latency",
			raw:      now,
			event:    earlier,
			wantZero: false,
		},
		{
			name:     "zero event time",
			raw:      now,
			event:    time.Time{},
			wantZero: true,
		},
		{
			name:     "invalid raw time",
			raw:      "invalid",
			event:    earlier,
			wantZero: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			latency := computeLatency(tt.raw, tt.event)
			if tt.wantZero && latency != 0 {
				t.Errorf("expected zero latency, got %v", latency)
			}
			if !tt.wantZero && latency == 0 {
				t.Error("expected non-zero latency")
			}
		})
	}
}

func TestIngestorInvalidInstrument(t *testing.T) {
	table := NewTable()
	route := Route{
		Type: "TICKER",
	}
	_ = table.Upsert(route)
	
	ingestor := NewIngestor(table)
	
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	rawStream := make(chan schema.RawInstance, 1)
	_, errs := ingestor.Run(ctx, rawStream)
	
	// Send event with invalid instrument
	rawStream <- schema.RawInstance{
		"canonicalType": "TICKER",
		"instrument":    "INVALID",  // No hyphen
		"source":        "binance",
		"ts":            time.Now().UnixMilli(),
	}
	close(rawStream)
	
	// Should receive error
	select {
	case err := <-errs:
		if err == nil {
			t.Error("expected error for invalid instrument")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for error")
	}
}

func TestIngestorContextCancellation(t *testing.T) {
	table := NewTable()
	ingestor := NewIngestor(table)
	
	ctx, cancel := context.WithCancel(context.Background())
	
	rawStream := make(chan schema.RawInstance)
	events, errs := ingestor.Run(ctx, rawStream)
	
	// Cancel context immediately
	cancel()
	
	// Channels should close
	_, eventsOpen := <-events
	_, errsOpen := <-errs
	
	if eventsOpen {
		t.Error("expected events channel to be closed")
	}
	if errsOpen {
		t.Error("expected errors channel to be closed")
	}
}
