package integration

import (
	"context"
	"sync"
	"time"
)

// FakeREST simulates a REST polling source for integration tests.
type FakeREST struct {
	mu       sync.Mutex
	payloads []map[string]any
	delay    time.Duration
}

// NewFakeREST constructs a fake REST source with optional response delay.
func NewFakeREST(delay time.Duration) *FakeREST {
	return &FakeREST{delay: delay}
}

// QueueResponse appends a REST response payload.
func (f *FakeREST) QueueResponse(payload map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payloads = append(f.payloads, payload)
}

// Poll streams queued responses respecting the configured delay.
func (f *FakeREST) Poll(ctx context.Context) <-chan map[string]any {
	out := make(chan map[string]any, len(f.payloads))
	go func() {
		defer close(out)
		f.mu.Lock()
		payloads := append([]map[string]any(nil), f.payloads...)
		f.payloads = nil
		f.mu.Unlock()

		for _, payload := range payloads {
			if f.delay > 0 {
				timer := time.NewTimer(f.delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			select {
			case <-ctx.Done():
				return
			case out <- payload:
			}
		}
	}()
	return out
}
