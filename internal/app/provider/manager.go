// Package provider manages provider instances and their lifecycle.
package provider

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/coachpo/meltica/internal/app/dispatcher"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/adapters/shared"
	"github.com/coachpo/meltica/internal/infra/config"
	"github.com/coachpo/meltica/internal/infra/pool"
)

// Manager owns provider instances materialised from the manifest.
type Manager struct {
	mu            sync.RWMutex
	registry      *Registry
	pools         *pool.PoolManager
	providers     map[string]Instance
	subscriptions map[string]*shared.SubscriptionManager
	specs         map[string]config.ProviderSpec
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
		specs:         make(map[string]config.ProviderSpec),
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
	m.specs[spec.Name] = spec
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

// ProviderMetadataSnapshot returns metadata for all running providers.
func (m *Manager) ProviderMetadataSnapshot() []RuntimeMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]RuntimeMetadata, 0, len(m.providers))
	for name, inst := range m.providers {
		spec := m.specs[name]
		instruments := inst.Instruments()
		runtime := buildRuntimeMetadata(spec, len(instruments))
		out = append(out, runtime)
	}
	SortRuntimeMetadata(out)
	return out
}

// ProviderMetadataFor returns detailed metadata for a provider instance.
func (m *Manager) ProviderMetadataFor(name string) (RuntimeDetail, bool) {
	m.mu.RLock()
	inst, ok := m.providers[name]
	if !ok {
		m.mu.RUnlock()
		var empty RuntimeDetail
		return empty, false
	}
	spec := m.specs[name]
	m.mu.RUnlock()
	instruments := inst.Instruments()
	meta := buildRuntimeMetadata(spec, len(instruments))
	adapterMeta, _ := m.registry.AdapterMetadata(spec.Exchange)
	return CloneRuntimeDetail(RuntimeDetail{
		RuntimeMetadata: meta,
		Instruments:     instruments,
		AdapterMetadata: adapterMeta,
	}), true
}

func buildRuntimeMetadata(spec config.ProviderSpec, instrumentCount int) RuntimeMetadata {
	settings := extractProviderSettings(spec.Config)
	meta := RuntimeMetadata{
		Name:            spec.Name,
		Exchange:        spec.Exchange,
		Identifier:      spec.Exchange,
		Settings:        settings,
		InstrumentCount: instrumentCount,
	}
	return CloneRuntimeMetadata(meta)
}

func extractProviderSettings(cfg map[string]any) map[string]any {
	if len(cfg) == 0 {
		return nil
	}
	settings := map[string]any{}
	for key, value := range cfg {
		switch key {
		case "identifier", "provider_name":
			continue
		case "config":
			nested, ok := value.(map[string]any)
			if !ok {
				continue
			}
			for nk, nv := range nested {
				settings[nk] = nv
			}
		default:
			settings[key] = value
		}
	}
	if len(settings) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(settings))
	for k, v := range settings {
		cloned[k] = v
	}
	return cloned
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
	if err := sub.Deactivate(ctx, route); err != nil {
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
