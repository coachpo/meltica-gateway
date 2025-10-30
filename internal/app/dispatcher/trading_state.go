package dispatcher

import (
	"strings"
	"sync"
)

// TradingState tracks per-consumer trading enablement.
type TradingState struct {
	mu    sync.RWMutex
	flags map[string]bool
}

// NewTradingState constructs an empty trading state store.
func NewTradingState() *TradingState {
	//nolint:exhaustruct // zero value for mu is intentional
	return &TradingState{
		flags: make(map[string]bool),
	}
}

// Set stores the trading flag for the consumer.
func (s *TradingState) Set(consumerID string, enabled bool) {
	if s == nil {
		return
	}
	consumerID = normalizeConsumerID(consumerID)
	if consumerID == "" {
		return
	}
	s.mu.Lock()
	s.flags[consumerID] = enabled
	s.mu.Unlock()
}

// Enabled reports whether trading is enabled for the consumer.
func (s *TradingState) Enabled(consumerID string) bool {
	if s == nil {
		return true
	}
	consumerID = normalizeConsumerID(consumerID)
	if consumerID == "" {
		return true
	}
	s.mu.RLock()
	enabled, ok := s.flags[consumerID]
	s.mu.RUnlock()
	if !ok {
		return true
	}
	return enabled
}

func normalizeConsumerID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}
