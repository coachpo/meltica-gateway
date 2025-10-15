package consumer

import (
	"context"
	"sync"

	"github.com/coachpo/meltica/pkg/events"
)

// Registry tracks consumer wrappers and provides safe invocation helpers.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*Wrapper
}

// NewRegistry constructs an empty consumer wrapper registry.
func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]*Wrapper)} //nolint:exhaustruct
}

// Register associates a consumer identifier with a wrapper instance.
func (r *Registry) Register(wrapper *Wrapper) {
	if wrapper == nil {
		return
	}
	r.mu.Lock()
	r.entries[wrapper.consumerID] = wrapper
	r.mu.Unlock()
}

// Invoke delegates event processing to the registered wrapper for the consumer.
func (r *Registry) Invoke(ctx context.Context, consumerID string, ev *events.Event, fn ConsumerFunc) error {
	wrapper := r.lookup(consumerID)
	if wrapper == nil {
		return nil
	}
	return wrapper.Invoke(ctx, ev, fn)
}

// UpdateMinVersion updates the routing version threshold for the consumer.
func (r *Registry) UpdateMinVersion(consumerID string, version uint64) {
	wrapper := r.lookup(consumerID)
	if wrapper == nil {
		return
	}
	wrapper.UpdateMinVersion(version)
}

func (r *Registry) lookup(consumerID string) *Wrapper {
	r.mu.RLock()
	wrapper := r.entries[consumerID]
	r.mu.RUnlock()
	return wrapper
}
