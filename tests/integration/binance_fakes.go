// Package integration contains integration test fixtures for Meltica.
package integration

import (
	"context"
	"sync"
	"time"

	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/schema"
)

// FakeFrameProvider replays websocket frames for tests.
type FakeFrameProvider struct {
	frames [][]byte
	mu     sync.Mutex
}

// Enqueue appends a new frame to the provider.
func (f *FakeFrameProvider) Enqueue(frame any) {
	data, _ := json.Marshal(frame)
	f.mu.Lock()
	f.frames = append(f.frames, data)
	f.mu.Unlock()
}

// Subscribe implements the FrameProvider interface expected by the WS client.
func (f *FakeFrameProvider) Subscribe(ctx context.Context, topics []string) (<-chan []byte, <-chan error, error) {
	out := make(chan []byte, len(f.frames))
	errCh := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errCh)
		f.mu.Lock()
		frames := append([][]byte(nil), f.frames...)
		f.frames = nil
		f.mu.Unlock()
		for _, frame := range frames {
			select {
			case <-ctx.Done():
				return
			case out <- frame:
			}
		}
	}()
	return out, errCh, nil
}

// FakeSnapshotProvider returns canned REST responses.
type FakeSnapshotProvider struct {
	responses [][]byte
	mu        sync.Mutex
}

// Add appends a new snapshot payload.
func (f *FakeSnapshotProvider) Add(payload any) {
	data, _ := json.Marshal(payload)
	f.mu.Lock()
	f.responses = append(f.responses, data)
	f.mu.Unlock()
}

// Fetch simulates a REST snapshot fetch.
func (f *FakeSnapshotProvider) Fetch(ctx context.Context, endpoint string) ([]byte, error) {
	f.mu.Lock()
	if len(f.responses) == 0 {
		f.mu.Unlock()
		return nil, context.Canceled
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	f.mu.Unlock()
	return resp, nil
}

// FakeSubscriber collects meltica events from the data bus.
type FakeSubscriber struct {
	events []schema.MelticaEvent
	mu     sync.Mutex
}

// Append records an event.
func (f *FakeSubscriber) Append(evt schema.MelticaEvent) {
	f.mu.Lock()
	f.events = append(f.events, evt)
	f.mu.Unlock()
}

// Events returns the recorded events.
func (f *FakeSubscriber) Events() []schema.MelticaEvent {
	f.mu.Lock()
	copy := append([]schema.MelticaEvent(nil), f.events...)
	f.mu.Unlock()
	return copy
}

// AwaitEvents waits until n events are received or the context expires.
func (f *FakeSubscriber) AwaitEvents(ctx context.Context, ch <-chan schema.MelticaEvent, n int) {
	count := 0
	for count < n {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			f.Append(evt)
			count++
		}
	}
}

// Delay allows tests to wait for asynchronous pipelines.
func Delay() {
	time.Sleep(10 * time.Millisecond)
}
