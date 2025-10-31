// Package provider manages provider instances and their lifecycle.
package provider

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/coachpo/meltica/internal/app/dispatcher"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/adapters/shared"
	"github.com/coachpo/meltica/internal/infra/bus/eventbus"
	"github.com/coachpo/meltica/internal/infra/config"
	"github.com/coachpo/meltica/internal/infra/pool"
)

// Manager owns provider instances materialised from the manifest.
type Manager struct {
	mu       sync.RWMutex
	registry *Registry
	pools    *pool.PoolManager
	bus      eventbus.Bus
	table    *dispatcher.Table
	logger   *log.Logger

	lifecycleMu  sync.RWMutex
	lifecycleCtx context.Context

	states map[string]*providerState
}

type providerState struct {
	spec          config.ProviderSpec
	instance      Instance
	subscriptions *shared.SubscriptionManager
	cancel        context.CancelFunc
	cachedRoutes  []dispatcher.Route
	running       bool
}

var (
	// ErrProviderExists indicates that a provider with the given name already exists.
	ErrProviderExists = errors.New("provider already exists")
	// ErrProviderNotFound indicates that the requested provider was not found.
	ErrProviderNotFound = errors.New("provider not found")
	// ErrProviderRunning indicates that the provider is already running.
	ErrProviderRunning = errors.New("provider already running")
	// ErrProviderNotRunning indicates that the provider is not currently running.
	ErrProviderNotRunning = errors.New("provider not running")
)

// NewManager creates a new provider manager.
func NewManager(reg *Registry, pools *pool.PoolManager, bus eventbus.Bus, table *dispatcher.Table, logger *log.Logger) *Manager {
	if reg == nil {
		reg = NewRegistry()
	}
	if logger == nil {
		logger = log.New(os.Stdout, "provider-manager ", log.LstdFlags|log.Lmicroseconds)
	}
	if table == nil {
		table = dispatcher.NewTable()
	}
	return &Manager{
		mu:           sync.RWMutex{},
		registry:     reg,
		pools:        pools,
		bus:          bus,
		table:        table,
		logger:       logger,
		lifecycleMu:  sync.RWMutex{},
		lifecycleCtx: context.Background(),
		states:       make(map[string]*providerState),
	}
}

// SetLifecycleContext configures the parent context for provider lifecycles.
func (m *Manager) SetLifecycleContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	m.lifecycleMu.Lock()
	m.lifecycleCtx = ctx
	m.lifecycleMu.Unlock()
}

func (m *Manager) parentContext() context.Context {
	m.lifecycleMu.RLock()
	ctx := m.lifecycleCtx
	m.lifecycleMu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (m *Manager) deriveProviderContext(ctx context.Context) (context.Context, context.CancelFunc) {
	parent := m.parentContext()

	if ctx == nil {
		return context.WithCancel(parent)
	}

	if ctx == parent {
		return context.WithCancel(ctx)
	}

	providerCtx, cancel := context.WithCancel(ctx)
	go func(parentCtx, run context.Context, cancelFunc context.CancelFunc) {
		select {
		case <-parentCtx.Done():
			cancelFunc()
		case <-run.Done():
		}
	}(parent, providerCtx, cancel)
	return providerCtx, cancel
}

// Registry exposes the underlying factory registry.
func (m *Manager) Registry() *Registry {
	return m.registry
}

// Start constructs all providers from the supplied specifications.
func (m *Manager) Start(ctx context.Context, specs []config.ProviderSpec) (map[string]Instance, error) {
	for _, spec := range specs {
		if _, err := m.Create(ctx, spec, true); err != nil {
			return nil, err
		}
	}
	return m.Providers(), nil
}

// Create registers a new provider specification and optionally starts it.
func (m *Manager) Create(ctx context.Context, spec config.ProviderSpec, start bool) (RuntimeDetail, error) {
	var empty RuntimeDetail
	spec = normalizeProviderSpec(spec)
	if spec.Name == "" {
		return empty, fmt.Errorf("provider name required")
	}

	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return empty, fmt.Errorf("context error: %w", err)
		}
	}

	m.mu.Lock()
	if _, exists := m.states[spec.Name]; exists {
		m.mu.Unlock()
		return empty, fmt.Errorf("%w: %s", ErrProviderExists, spec.Name)
	}
	state := &providerState{
		spec:          spec,
		instance:      nil,
		subscriptions: nil,
		cancel:        nil,
		cachedRoutes:  nil,
		running:       false,
	}
	m.states[spec.Name] = state
	m.mu.Unlock()

	if start {
		if _, err := m.StartProvider(ctx, spec.Name); err != nil {
			m.mu.Lock()
			delete(m.states, spec.Name)
			m.mu.Unlock()
			return empty, err
		}
	}

	detail, ok := m.ProviderMetadataFor(spec.Name)
	if !ok {
		return empty, fmt.Errorf("%w: %s", ErrProviderNotFound, spec.Name)
	}
	return detail, nil
}

// Update replaces an existing provider specification and optionally restarts it.
func (m *Manager) Update(ctx context.Context, spec config.ProviderSpec, start bool) (RuntimeDetail, error) {
	var empty RuntimeDetail
	spec = normalizeProviderSpec(spec)
	if spec.Name == "" {
		return empty, fmt.Errorf("provider name required")
	}

	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return empty, fmt.Errorf("context error: %w", err)
		}
	}

	m.mu.Lock()
	state, ok := m.states[spec.Name]
	if !ok {
		m.mu.Unlock()
		return empty, fmt.Errorf("%w: %s", ErrProviderNotFound, spec.Name)
	}
	wasRunning := state.running
	if wasRunning {
		m.stopProviderLocked(state)
	}
	state.spec = spec
	m.mu.Unlock()

	if start {
		if _, err := m.StartProvider(ctx, spec.Name); err != nil {
			return empty, err
		}
	}

	detail, ok := m.ProviderMetadataFor(spec.Name)
	if !ok {
		return empty, fmt.Errorf("%w: %s", ErrProviderNotFound, spec.Name)
	}
	return detail, nil
}

// Remove stops and deletes a provider specification.
func (m *Manager) Remove(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("provider name required")
	}

	m.mu.Lock()
	state, ok := m.states[trimmed]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrProviderNotFound, trimmed)
	}
	m.stopProviderLocked(state)
	delete(m.states, trimmed)
	m.mu.Unlock()
	return nil
}

// StartProvider starts a configured provider instance.
func (m *Manager) StartProvider(ctx context.Context, name string) (RuntimeDetail, error) {
	var empty RuntimeDetail
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return empty, fmt.Errorf("provider name required")
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return empty, fmt.Errorf("context error: %w", err)
		}
	}

	m.mu.Lock()
	state, ok := m.states[trimmed]
	if !ok {
		m.mu.Unlock()
		return empty, fmt.Errorf("%w: %s", ErrProviderNotFound, trimmed)
	}
	if state.running {
		m.mu.Unlock()
		return empty, fmt.Errorf("%w: %s", ErrProviderRunning, trimmed)
	}
	err := m.startProviderLocked(ctx, state)
	m.mu.Unlock()
	if err != nil {
		return empty, err
	}
	detail, ok := m.ProviderMetadataFor(trimmed)
	if !ok {
		return empty, fmt.Errorf("%w: %s", ErrProviderNotFound, trimmed)
	}
	return detail, nil
}

// StopProvider stops a running provider instance but retains its specification.
func (m *Manager) StopProvider(name string) (RuntimeDetail, error) {
	var empty RuntimeDetail
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return empty, fmt.Errorf("provider name required")
	}

	m.mu.Lock()
	state, ok := m.states[trimmed]
	if !ok {
		m.mu.Unlock()
		return empty, fmt.Errorf("%w: %s", ErrProviderNotFound, trimmed)
	}
	if !state.running {
		m.mu.Unlock()
		return empty, fmt.Errorf("%w: %s", ErrProviderNotRunning, trimmed)
	}
	m.stopProviderLocked(state)
	m.mu.Unlock()
	detail, ok := m.ProviderMetadataFor(trimmed)
	if !ok {
		return empty, fmt.Errorf("%w: %s", ErrProviderNotFound, trimmed)
	}
	return detail, nil
}

func (m *Manager) startProviderLocked(ctx context.Context, state *providerState) error {
	if state == nil {
		return fmt.Errorf("provider state required")
	}
	providerCtx, cancel := m.deriveProviderContext(ctx)
	instance, err := m.registry.Create(providerCtx, m.pools, state.spec)
	if err != nil {
		cancel()
		return err
	}
	state.instance = instance
	state.cancel = cancel
	state.subscriptions = shared.NewSubscriptionManager(instance)
	state.running = true

	if len(state.cachedRoutes) > 0 {
		for _, route := range state.cachedRoutes {
			route.Provider = state.spec.Name
			if err := state.subscriptions.Activate(providerCtx, route); err != nil && m.logger != nil {
				m.logger.Printf("provider/%s: reapply route %s: %v", state.spec.Name, route.Type, err)
			}
		}
	}

	m.logErrors(providerCtx, fmt.Sprintf("provider/%s", state.spec.Name), instance.Errors())
	if m.bus != nil {
		runtime := dispatcher.NewRuntime(m.bus, m.table, m.pools)
		errCh := runtime.Start(providerCtx, instance.Events())
		m.logErrors(providerCtx, fmt.Sprintf("dispatcher/%s", state.spec.Name), errCh)
	}
	return nil
}

func (m *Manager) stopProviderLocked(state *providerState) {
	if state == nil || !state.running {
		return
	}
	if state.subscriptions != nil {
		state.cachedRoutes = cloneRoutes(state.subscriptions.Snapshot())
	}
	if state.cancel != nil {
		state.cancel()
	}
	state.cancel = nil
	state.instance = nil
	state.subscriptions = nil
	state.running = false
}

func (m *Manager) logErrors(ctx context.Context, stage string, errs <-chan error) {
	if errs == nil || m.logger == nil {
		return
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-errs:
				if !ok {
					return
				}
				if err != nil {
					m.logger.Printf("%s: %v", stage, err)
				}
			}
		}
	}()
}

func cloneRoutes(routes []dispatcher.Route) []dispatcher.Route {
	if len(routes) == 0 {
		return nil
	}
	out := make([]dispatcher.Route, len(routes))
	for i, route := range routes {
		out[i] = cloneRoute(route)
	}
	return out
}

func cloneRoute(route dispatcher.Route) dispatcher.Route {
	cloned := route
	if len(route.WSTopics) > 0 {
		cloned.WSTopics = append([]string(nil), route.WSTopics...)
	}
	if len(route.RestFns) > 0 {
		cloned.RestFns = append([]dispatcher.RestFn(nil), route.RestFns...)
	}
	if len(route.Filters) > 0 {
		filters := make([]dispatcher.FilterRule, len(route.Filters))
		copy(filters, route.Filters)
		cloned.Filters = filters
	}
	return cloned
}

func normalizeProviderSpec(spec config.ProviderSpec) config.ProviderSpec {
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Adapter = strings.TrimSpace(spec.Adapter)
	spec.Config = cloneConfigMap(spec.Config)
	return spec
}

func cloneConfigMap(cfg map[string]any) map[string]any {
	if len(cfg) == 0 {
		return nil
	}
	clone := make(map[string]any, len(cfg))
	for k, v := range cfg {
		clone[k] = v
	}
	return clone
}

// Providers returns a copy of the provider map.
func (m *Manager) Providers() map[string]Instance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]Instance, len(m.states))
	for name, state := range m.states {
		if state.running && state.instance != nil {
			out[name] = state.instance
		}
	}
	return out
}

// Provider resolves a provider instance by name.
func (m *Manager) Provider(name string) (Instance, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, false
	}
	m.mu.RLock()
	state, ok := m.states[trimmed]
	var inst Instance
	var running bool
	if ok {
		inst = state.instance
		running = state.running && inst != nil
	}
	m.mu.RUnlock()
	if !ok || !running {
		return nil, false
	}
	return inst, true
}

// ProviderMetadataSnapshot returns metadata for all running providers.
func (m *Manager) ProviderMetadataSnapshot() []RuntimeMetadata {
	m.mu.RLock()
	out := make([]RuntimeMetadata, 0, len(m.states))
	for _, state := range m.states {
		instrumentCount := 0
		if state.running && state.instance != nil {
			instrumentCount = len(state.instance.Instruments())
		}
		runtime := buildRuntimeMetadata(state.spec, instrumentCount, state.running)
		out = append(out, runtime)
	}
	m.mu.RUnlock()
	SortRuntimeMetadata(out)
	return out
}

// ProviderSpecsSnapshot returns a copy of the provider specifications known to the manager.
func (m *Manager) ProviderSpecsSnapshot() []config.ProviderSpec {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.states) == 0 {
		return nil
	}
	specs := make([]config.ProviderSpec, 0, len(m.states))
	for _, state := range m.states {
		clone := config.ProviderSpec{
			Name:    state.spec.Name,
			Adapter: state.spec.Adapter,
			Config:  cloneConfigMap(state.spec.Config),
		}
		specs = append(specs, clone)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

// ProviderMetadataFor returns detailed metadata for a provider instance.
func (m *Manager) ProviderMetadataFor(name string) (RuntimeDetail, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		var empty RuntimeDetail
		return empty, false
	}
	m.mu.RLock()
	state, ok := m.states[trimmed]
	var spec config.ProviderSpec
	var instance Instance
	var running bool
	if ok {
		spec = state.spec
		instance = state.instance
		running = state.running && instance != nil
	}
	m.mu.RUnlock()
	if !ok {
		var empty RuntimeDetail
		return empty, false
	}
	instruments := []schema.Instrument(nil)
	instrumentCount := 0
	if running {
		instruments = instance.Instruments()
		instrumentCount = len(instruments)
	}
	meta := buildRuntimeMetadata(spec, instrumentCount, running)
	adapterMeta, _ := m.registry.AdapterMetadata(spec.Adapter)
	detail := RuntimeDetail{
		RuntimeMetadata: meta,
		Instruments:     instruments,
		AdapterMetadata: adapterMeta,
	}
	if !running {
		detail.Instruments = nil
	}
	return CloneRuntimeDetail(detail), true
}

func buildRuntimeMetadata(spec config.ProviderSpec, instrumentCount int, running bool) RuntimeMetadata {
	settings := extractProviderSettings(spec.Config)
	meta := RuntimeMetadata{
		Name:            spec.Name,
		Adapter:         spec.Adapter,
		Identifier:      spec.Adapter,
		Settings:        settings,
		InstrumentCount: instrumentCount,
		Running:         running,
	}
	return CloneRuntimeMetadata(meta)
}

func extractProviderSettings(cfg map[string]any) map[string]any {
	if len(cfg) == 0 {
		return nil
	}
	settings := make(map[string]any)
	for key, value := range cfg {
		if shouldOmitProviderSettingKey(key) {
			continue
		}

		switch strings.ToLower(strings.TrimSpace(key)) {
		case "identifier", "provider_name":
			continue
		case "config":
			nested, ok := value.(map[string]any)
			if !ok {
				continue
			}
			clean := sanitizeProviderSettingsMap(nested)
			if len(clean) == 0 {
				continue
			}
			for nk, nv := range clean {
				settings[nk] = nv
			}
		default:
			sanitized := sanitizeProviderSettingValue(value)
			if sanitized == nil {
				continue
			}
			settings[key] = sanitized
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
	state, ok := m.states[providerName]
	var sub *shared.SubscriptionManager
	var running bool
	if ok {
		sub = state.subscriptions
		running = state.running && sub != nil
	}
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrProviderNotFound, providerName)
	}
	if !running {
		return fmt.Errorf("%w: %s", ErrProviderNotRunning, providerName)
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
	state, ok := m.states[providerName]
	var sub *shared.SubscriptionManager
	if ok {
		sub = state.subscriptions
	}
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrProviderNotFound, providerName)
	}
	if sub == nil {
		return fmt.Errorf("%w: %s", ErrProviderNotRunning, providerName)
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
	m.mu.RLock()
	state, ok := m.states[providerName]
	var inst Instance
	var running bool
	if ok {
		inst = state.instance
		running = state.running && inst != nil
	}
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrProviderNotFound, providerName)
	}
	if !running {
		return fmt.Errorf("%w: %s", ErrProviderNotRunning, providerName)
	}
	if err := inst.SubmitOrder(ctx, req); err != nil {
		return fmt.Errorf("submit order to provider %q: %w", providerName, err)
	}
	return nil
}
