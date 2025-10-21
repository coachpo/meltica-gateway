// Package provider manages provider instances and their lifecycle.
package provider

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/coachpo/meltica/internal/adapters/shared"
	"github.com/coachpo/meltica/internal/config"
	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

// Manager owns provider instances materialised from the manifest.
type Manager struct {
	mu            sync.RWMutex
	registry      *Registry
	pools         *pool.PoolManager
	providers     map[string]Instance
	subscriptions map[string]*shared.SubscriptionManager
}

// NewManager creates a new provider manager.
func NewManager(reg *Registry, pools *pool.PoolManager) *Manager {
	if reg == nil {
		reg = NewRegistry()
	}
	return &Manager{
		mu:            sync.RWMutex{},
		registry:      reg,
		pools:         pools,
		providers:     make(map[string]Instance),
		subscriptions: make(map[string]*shared.SubscriptionManager),
	}
}

// Registry exposes the underlying factory registry.
func (m *Manager) Registry() *Registry {
	return m.registry
}

// Start constructs all providers from the supplied specifications.
func (m *Manager) Start(ctx context.Context, specs []config.ProviderSpec) (map[string]Instance, error) {
	for _, spec := range specs {
		if err := m.addProvider(ctx, spec); err != nil {
			return nil, err
		}
	}
	return m.Providers(), nil
}

func (m *Manager) addProvider(ctx context.Context, spec config.ProviderSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.providers[spec.Name]; exists {
		return fmt.Errorf("provider %q already exists", spec.Name)
	}
	instance, err := m.registry.Create(ctx, m.pools, spec)
	if err != nil {
		return err
	}
	m.providers[spec.Name] = instance
	m.subscriptions[spec.Name] = shared.NewSubscriptionManager(instance)
	return nil
}

// Providers returns a copy of the provider map.
func (m *Manager) Providers() map[string]Instance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]Instance, len(m.providers))
	for name, provider := range m.providers {
		out[name] = provider
	}
	return out
}

// Provider resolves a provider instance by name.
func (m *Manager) Provider(name string) (Instance, bool) {
	m.mu.RLock()
	inst, ok := m.providers[name]
	m.mu.RUnlock()
	return inst, ok
}

// ActivateRoute applies a route update to the targeted provider only.
func (m *Manager) ActivateRoute(ctx context.Context, route dispatcher.Route) error {
	providerName := strings.TrimSpace(route.Provider)
	if providerName == "" {
		return fmt.Errorf("route provider required")
	}
	m.mu.RLock()
	sub, ok := m.subscriptions[providerName]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("provider %q not found", providerName)
	}
	if err := sub.Activate(ctx, route); err != nil {
		return fmt.Errorf("activate route: %w", err)
	}
	return nil
}

// DeactivateRoute removes a route from the targeted provider only.
func (m *Manager) DeactivateRoute(ctx context.Context, route dispatcher.Route) error {
	providerName := strings.TrimSpace(route.Provider)
	if providerName == "" {
		return fmt.Errorf("route provider required")
	}
	m.mu.RLock()
	sub, ok := m.subscriptions[providerName]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("provider %q not found", providerName)
	}
	if err := sub.Deactivate(ctx, route.Type); err != nil {
		return fmt.Errorf("deactivate route: %w", err)
	}
	return nil
}

// SubmitOrder delegates order submission to the addressed provider.
func (m *Manager) SubmitOrder(ctx context.Context, req schema.OrderRequest) error {
	providerName := strings.TrimSpace(req.Provider)
	if providerName == "" {
		return fmt.Errorf("order provider required")
	}
	inst, ok := m.Provider(providerName)
	if !ok {
		return fmt.Errorf("provider %q not found", providerName)
	}
	if err := inst.SubmitOrder(ctx, req); err != nil {
		return fmt.Errorf("submit order to provider %q: %w", providerName, err)
	}
	return nil
}
