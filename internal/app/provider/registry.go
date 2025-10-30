package provider

import (
	"context"
	"fmt"
	"sync"

	"github.com/coachpo/meltica/internal/infra/config"
	"github.com/coachpo/meltica/internal/infra/pool"
)

// Factory constructs a provider instance from the manifest specification.
type Factory func(ctx context.Context, pools *pool.PoolManager, cfg map[string]any) (Instance, error)

// Registry maintains provider factories keyed by manifest type.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// NewRegistry creates a new provider factory registry.
func NewRegistry() *Registry {
	return &Registry{
		mu:        sync.RWMutex{},
		factories: make(map[string]Factory),
	}
}

// Register registers a provider factory for the given type.
func (r *Registry) Register(typ string, factory Factory) {
	if factory == nil {
		panic("provider factory required")
	}
	r.mu.Lock()
	r.factories[typ] = factory
	r.mu.Unlock()
}

// Create creates a provider instance from the specification.
func (r *Registry) Create(ctx context.Context, pools *pool.PoolManager, spec config.ProviderSpec) (Instance, error) {
	r.mu.RLock()
	factory, ok := r.factories[spec.Exchange]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("provider exchange %q not registered", spec.Exchange)
	}
	instance, err := factory(ctx, pools, spec.Config)
	if err != nil {
		return nil, fmt.Errorf("instantiate provider %s(%s): %w", spec.Name, spec.Exchange, err)
	}
	return instance, nil
}
