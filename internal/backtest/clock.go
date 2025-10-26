package backtest

import (
	"sync"
	"time"
)

// Clock provides a controllable notion of time for deterministic simulations.
type Clock interface {
	Now() time.Time
	Advance(d time.Duration)
	AdvanceTo(ts time.Time)
}

// VirtualClock is an in-memory clock implementation used during backtests.
type VirtualClock struct {
	mu      sync.Mutex
	current time.Time
}

// NewVirtualClock initialises a clock starting at the provided timestamp.
func NewVirtualClock(start time.Time) *VirtualClock {
	return &VirtualClock{current: start}
}

// Now returns the current simulated time.
func (c *VirtualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// Advance moves the clock forward by the specified duration.
func (c *VirtualClock) Advance(d time.Duration) {
	if d <= 0 {
		return
	}
	c.mu.Lock()
	c.current = c.current.Add(d)
	c.mu.Unlock()
}

// AdvanceTo moves the clock to the supplied timestamp if it is in the future.
func (c *VirtualClock) AdvanceTo(ts time.Time) {
	c.mu.Lock()
	if ts.After(c.current) {
		c.current = ts
	}
	c.mu.Unlock()
}
