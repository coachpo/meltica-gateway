package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	metadata  map[string]AdapterMetadata
}

// NewRegistry creates a new provider factory registry.
func NewRegistry() *Registry {
	return &Registry{
		mu:        sync.RWMutex{},
		factories: make(map[string]Factory),
		metadata:  make(map[string]AdapterMetadata),
	}
}

// Register registers a provider factory for the given type.
func (r *Registry) Register(typ string, factory Factory) {
	if factory == nil {
		panic("provider factory required")
	}
	var empty AdapterMetadata
	r.RegisterWithMetadata(typ, factory, empty)
}

// RegisterWithMetadata registers a provider factory with accompanying adapter metadata.
func (r *Registry) RegisterWithMetadata(typ string, factory Factory, meta AdapterMetadata) {
	if factory == nil {
		panic("provider factory required")
	}
	r.mu.Lock()
	r.factories[typ] = factory
	if strings.TrimSpace(meta.Identifier) == "" {
		delete(r.metadata, typ)
	} else {
		cloned := meta.Clone()
		r.metadata[typ] = cloned
	}
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

// AdapterMetadata returns metadata for a registered adapter.
func (r *Registry) AdapterMetadata(identifier string) (AdapterMetadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	meta, ok := r.metadata[identifier]
	if !ok {
		var empty AdapterMetadata
		return empty, false
	}
	return meta.Clone(), true
}

// AdapterMetadataSnapshot returns metadata for all registered adapters.
func (r *Registry) AdapterMetadataSnapshot() []AdapterMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AdapterMetadata, 0, len(r.metadata))
	for _, meta := range r.metadata {
		out = append(out, meta.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identifier < out[j].Identifier })
	return out
}
