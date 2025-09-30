package integration

import (
	"context"
	"sync"
)

// FakeWebSocket simulates a websocket stream for integration tests.
type FakeWebSocket struct {
	mu     sync.Mutex
	frames chan []byte
	errs   chan error
	closed bool
}

// NewFakeWebSocket creates a fake websocket stream with buffered channels.
func NewFakeWebSocket(buffer int) *FakeWebSocket {
	if buffer <= 0 {
		buffer = 16
	}
	return &FakeWebSocket{
		frames: make(chan []byte, buffer),
		errs:   make(chan error, 1),
	}
}

// Stream returns the frame and error channels for consumers.
func (f *FakeWebSocket) Stream(ctx context.Context) (<-chan []byte, <-chan error) {
	frameCh := make(chan []byte, cap(f.frames))
	errCh := make(chan error, cap(f.errs))

	go func() {
		defer close(frameCh)
		defer close(errCh)
		for {
			select {
			case <-ctx.Done():
				return
			case frame, ok := <-f.frames:
				if !ok {
					return
				}
				frameCh <- frame
			case err, ok := <-f.errs:
				if !ok {
					return
				}
				errCh <- err
			}
		}
	}()
	return frameCh, errCh
}

// Publish enqueues a websocket frame for consumers.
func (f *FakeWebSocket) Publish(frame []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.frames <- append([]byte(nil), frame...)
}

// Fail enqueues an error for consumers.
func (f *FakeWebSocket) Fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.errs <- err
}

// Close closes the fake websocket channels.
func (f *FakeWebSocket) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	close(f.frames)
	close(f.errs)
	f.closed = true
}
