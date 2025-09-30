package conductor

import (
	"sync"
	"time"
)

// Throttle ensures fused events are emitted at most once per interval per instrument.
type Throttle struct {
	interval time.Duration
	mu       sync.Mutex
	last     map[string]time.Time
}

// NewThrottle constructs a throttle with the provided interval.
func NewThrottle(interval time.Duration) *Throttle {
	throttle := new(Throttle)
	throttle.interval = interval
	throttle.last = make(map[string]time.Time)
	return throttle
}

// Allow returns true if an event for the instrument should be emitted now.
func (t *Throttle) Allow(instrument string) bool {
	if t == nil {
		return true
	}
	if t.interval <= 0 {
		return true
	}
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	last, ok := t.last[instrument]
	if !ok || now.Sub(last) >= t.interval {
		t.last[instrument] = now
		return true
	}
	return false
}
