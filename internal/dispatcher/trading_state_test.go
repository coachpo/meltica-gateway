package dispatcher

import (
	"fmt"
	"testing"
)

func TestNewTradingState(t *testing.T) {
	state := NewTradingState()
	
	if state == nil {
		t.Fatal("expected non-nil trading state")
	}
	
	if state.flags == nil {
		t.Error("expected initialized flags map")
	}
}

func TestTradingStateSetAndEnabled(t *testing.T) {
	state := NewTradingState()
	
	// Default should be enabled (true) for unknown consumers
	if !state.Enabled("consumer1") {
		t.Error("expected default enabled state to be true")
	}
	
	// Set to false
	state.Set("consumer1", false)
	
	if state.Enabled("consumer1") {
		t.Error("expected consumer1 to be disabled")
	}
	
	// Set to true
	state.Set("consumer1", true)
	
	if !state.Enabled("consumer1") {
		t.Error("expected consumer1 to be enabled")
	}
}

func TestTradingStateNormalization(t *testing.T) {
	state := NewTradingState()
	
	// Set with uppercase
	state.Set("CONSUMER1", false)
	
	// Query with lowercase
	if state.Enabled("consumer1") {
		t.Error("expected normalized consumer ID to be disabled")
	}
	
	// Query with mixed case
	if state.Enabled("Consumer1") {
		t.Error("expected normalized consumer ID to be disabled")
	}
	
	// Set with whitespace
	state.Set("  consumer2  ", true)
	
	// Query without whitespace
	if !state.Enabled("consumer2") {
		t.Error("expected normalized consumer ID to be enabled")
	}
}

func TestTradingStateEmptyConsumerID(t *testing.T) {
	state := NewTradingState()
	
	// Empty ID should not panic
	state.Set("", false)
	
	// Empty ID should return default (true)
	if !state.Enabled("") {
		t.Error("expected empty consumer ID to return default true")
	}
	
	// Whitespace-only ID should behave like empty
	state.Set("   ", false)
	
	if !state.Enabled("   ") {
		t.Error("expected whitespace-only consumer ID to return default true")
	}
}

func TestTradingStateNilReceiver(t *testing.T) {
	var state *TradingState
	
	// Should not panic on nil receiver
	state.Set("consumer1", false)
	
	// Should return default (true) on nil receiver
	if !state.Enabled("consumer1") {
		t.Error("expected nil trading state to return default true")
	}
}

func TestTradingStateMultipleConsumers(t *testing.T) {
	state := NewTradingState()
	
	state.Set("consumer1", true)
	state.Set("consumer2", false)
	state.Set("consumer3", true)
	
	if !state.Enabled("consumer1") {
		t.Error("expected consumer1 to be enabled")
	}
	
	if state.Enabled("consumer2") {
		t.Error("expected consumer2 to be disabled")
	}
	
	if !state.Enabled("consumer3") {
		t.Error("expected consumer3 to be enabled")
	}
}

func TestTradingStateConcurrency(t *testing.T) {
	state := NewTradingState()
	
	done := make(chan bool)
	
	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func(id int) {
			consumerID := fmt.Sprintf("consumer%d", id)
			state.Set(consumerID, id%2 == 0)
			done <- true
		}(i)
	}
	
	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func(id int) {
			consumerID := fmt.Sprintf("consumer%d", id)
			_ = state.Enabled(consumerID)
			done <- true
		}(i)
	}
	
	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}
}

func TestNormalizeConsumerID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "lowercase",
			input: "consumer1",
			want:  "consumer1",
		},
		{
			name:  "uppercase",
			input: "CONSUMER1",
			want:  "consumer1",
		},
		{
			name:  "mixed case",
			input: "Consumer1",
			want:  "consumer1",
		},
		{
			name:  "with leading whitespace",
			input: "  consumer1",
			want:  "consumer1",
		},
		{
			name:  "with trailing whitespace",
			input: "consumer1  ",
			want:  "consumer1",
		},
		{
			name:  "with both whitespace",
			input: "  consumer1  ",
			want:  "consumer1",
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace only",
			input: "   ",
			want:  "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeConsumerID(tt.input)
			if got != tt.want {
				t.Errorf("normalizeConsumerID() = %q, want %q", got, tt.want)
			}
		})
	}
}
