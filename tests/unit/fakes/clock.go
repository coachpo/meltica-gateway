package fakes

import (
	"sync"
	"time"
)

// FakeClock provides deterministic time control for unit tests.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock constructs a fake clock initialized to the provided time.
func NewFakeClock(start time.Time) *FakeClock {
	if start.IsZero() {
		start = time.Unix(0, 0)
	}
	return &FakeClock{now: start}
}

// Now returns the current fake time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance increments the fake time by the provided duration.
func (c *FakeClock) Advance(delta time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(delta)
	c.mu.Unlock()
}

// After returns a channel that receives once the fake clock advances by the duration.
func (c *FakeClock) After(delta time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	go func() {
		target := c.Now().Add(delta)
		for {
			c.mu.Lock()
			current := c.now
			c.mu.Unlock()
			if !current.Before(target) {
				ch <- current
				close(ch)
				return
			}
			time.Sleep(1 * time.Millisecond)
		}
	}()
	return ch
}
