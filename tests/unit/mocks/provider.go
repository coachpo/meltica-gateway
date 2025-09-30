package mocks

import (
	"context"
	"sync"

	"github.com/coachpo/meltica/internal/schema"
)

// ProviderMock captures canonical events published by dispatcher tests.
type ProviderMock struct {
	mu     sync.Mutex
	events []schema.Event
	errs   []error
}

// Publish records an event in the mock stream.
func (m *ProviderMock) Publish(_ context.Context, evt schema.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, evt)
	return nil
}

// Fail records an error produced by the provider mock.
func (m *ProviderMock) Fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errs = append(m.errs, err)
}

// Events returns the recorded canonical events.
func (m *ProviderMock) Events() []schema.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]schema.Event, len(m.events))
	copy(out, m.events)
	return out
}

// Errors returns the recorded errors emitted by the provider mock.
func (m *ProviderMock) Errors() []error {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]error, len(m.errs))
	copy(out, m.errs)
	return out
}

// Reset clears recorded events and errors.
func (m *ProviderMock) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = nil
	m.errs = nil
}
