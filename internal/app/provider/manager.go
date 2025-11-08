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
	"github.com/coachpo/meltica/internal/domain/providerstore"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/adapters/shared"
	"github.com/coachpo/meltica/internal/infra/bus/eventbus"
	"github.com/coachpo/meltica/internal/infra/config"
	"github.com/coachpo/meltica/internal/infra/pool"
	"github.com/coachpo/meltica/internal/infra/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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

	persistence providerstore.Store
	states      map[string]*providerState

	cacheHitCounter  metric.Int64Counter
	cacheMissCounter metric.Int64Counter
}

// Option configures optional manager behaviour.
type Option func(*Manager)

// WithPersistence wires a provider persistence store into the manager.
func WithPersistence(store providerstore.Store) Option {
	return func(m *Manager) {
		m.persistence = store
	}
}

type providerState struct {
	spec              config.ProviderSpec
	instance          Instance
	subscriptions     *shared.SubscriptionManager
	cancel            context.CancelFunc
	cachedRoutes      []dispatcher.Route
	cachedInstruments []schema.Instrument
	running           bool
	status            Status
	startupErr        error
}

func statusFromString(value string) Status {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(StatusPending):
		return StatusPending
	case string(StatusStarting):
		return StatusStarting
	case string(StatusRunning):
		return StatusRunning
	case string(StatusStopped):
		return StatusStopped
	case string(StatusFailed):
		return StatusFailed
	default:
		return StatusPending
	}
}

// Status represents the lifecycle state of a provider.
type Status string

const (
	// StatusPending indicates the provider is registered but not yet started.
	StatusPending Status = "pending"
	// StatusStarting indicates the provider is currently starting.
	StatusStarting Status = "starting"
	// StatusRunning indicates the provider is fully operational.
	StatusRunning Status = "running"
	// StatusStopped indicates the provider has been stopped.
	StatusStopped Status = "stopped"
	// StatusFailed indicates the provider failed to start.
	StatusFailed Status = "failed"
)

const providerMetadataCacheName = "provider_metadata"

var (
	// ErrProviderExists indicates that a provider with the given name already exists.
	ErrProviderExists = errors.New("provider already exists")
	// ErrProviderNotFound indicates that the requested provider was not found.
	ErrProviderNotFound = errors.New("provider not found")
	// ErrProviderRunning indicates that the provider is already running.
	ErrProviderRunning = errors.New("provider already running")
	// ErrProviderStarting indicates that the provider is currently starting.
	ErrProviderStarting = errors.New("provider starting")
	// ErrProviderNotRunning indicates that the provider is not currently running.
	ErrProviderNotRunning = errors.New("provider not running")
)

// NewManager creates a new provider manager.
func NewManager(reg *Registry, pools *pool.PoolManager, bus eventbus.Bus, table *dispatcher.Table, logger *log.Logger, opts ...Option) *Manager {
	if reg == nil {
		reg = NewRegistry()
	}
	if logger == nil {
		logger = log.New(os.Stdout, "provider-manager ", log.LstdFlags|log.Lmicroseconds)
	}
	if table == nil {
		table = dispatcher.NewTable()
	}
	manager := &Manager{
		mu:               sync.RWMutex{},
		registry:         reg,
		pools:            pools,
		bus:              bus,
		table:            table,
		logger:           logger,
		lifecycleMu:      sync.RWMutex{},
		lifecycleCtx:     context.Background(),
		states:           make(map[string]*providerState),
		persistence:      nil,
		cacheHitCounter:  nil,
		cacheMissCounter: nil,
	}
	manager.initCacheMetrics()
	for _, opt := range opts {
		if opt != nil {
			opt(manager)
		}
	}
	return manager
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

// Registry exposes the underlying factory registry.
func (m *Manager) Registry() *Registry {
	return m.registry
}

// HasProvider reports whether a provider with the given name has been registered.
func (m *Manager) HasProvider(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false
	}
	m.mu.RLock()
	_, ok := m.states[trimmed]
	m.mu.RUnlock()
	return ok
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
		spec:              spec,
		instance:          nil,
		subscriptions:     nil,
		cancel:            nil,
		cachedRoutes:      nil,
		cachedInstruments: nil,
		running:           false,
		status:            StatusPending,
		startupErr:        nil,
	}
	m.states[spec.Name] = state
	m.mu.Unlock()

	m.persistSnapshot(spec.Name)

	if start {
		if _, err := m.StartProvider(ctx, spec.Name); err != nil {
			m.mu.Lock()
			delete(m.states, spec.Name)
			m.mu.Unlock()
			m.deleteSnapshot(spec.Name)
			m.deleteRoutes(spec.Name)
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
	if state.status == StatusStarting {
		m.mu.Unlock()
		return empty, fmt.Errorf("%w: %s", ErrProviderStarting, spec.Name)
	}
	wasRunning := state.running
	if wasRunning {
		m.stopProviderLocked(state)
	}
	state.spec = spec
	m.mu.Unlock()
	if wasRunning {
		m.persistRoutes(spec.Name)
	}

	m.persistSnapshot(spec.Name)

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

	m.deleteSnapshot(trimmed)
	m.deleteRoutes(trimmed)
	return nil
}

// StartProvider starts a configured provider instance synchronously.
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

	spec, cachedRoutes, err := m.prepareProviderStart(trimmed)
	if err != nil {
		return empty, err
	}

	if err := m.startProviderRuntime(trimmed, spec, cachedRoutes); err != nil {
		return empty, err
	}

	detail, ok := m.ProviderMetadataFor(trimmed)
	if !ok {
		return empty, fmt.Errorf("%w: %s", ErrProviderNotFound, trimmed)
	}
	return detail, nil
}

// StartProviderAsync starts a configured provider instance asynchronously.
func (m *Manager) StartProviderAsync(name string) (RuntimeDetail, error) {
	var empty RuntimeDetail
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return empty, fmt.Errorf("provider name required")
	}

	spec, cachedRoutes, err := m.prepareProviderStart(trimmed)
	if err != nil {
		return empty, err
	}

	go m.startProviderAsync(trimmed, spec, cachedRoutes)

	detail, ok := m.ProviderMetadataFor(trimmed)
	if !ok {
		return empty, fmt.Errorf("%w: %s", ErrProviderNotFound, trimmed)
	}
	return detail, nil
}

func (m *Manager) startProviderAsync(name string, spec config.ProviderSpec, cachedRoutes []dispatcher.Route) {
	if err := m.startProviderRuntime(name, spec, cachedRoutes); err != nil && m.logger != nil {
		m.logger.Printf("provider/%s: async start failed: %v", name, err)
	}
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

	m.persistSnapshot(trimmed)
	m.persistRoutes(trimmed)
	detail, ok := m.ProviderMetadataFor(trimmed)
	if !ok {
		return empty, fmt.Errorf("%w: %s", ErrProviderNotFound, trimmed)
	}
	return detail, nil
}

func (m *Manager) startProviderRuntime(name string, spec config.ProviderSpec, cachedRoutes []dispatcher.Route) error {
	parent := m.parentContext()
	providerCtx, cancel := context.WithCancel(parent)
	instance, err := m.registry.Create(providerCtx, m.pools, spec)
	if err != nil {
		cancel()
		m.recordProviderStartFailure(name, err)
		return err
	}

	subscriptions := shared.NewSubscriptionManager(instance)
	if !m.recordProviderStartSuccess(name, cancel, instance, subscriptions) {
		cancel()
		return fmt.Errorf("%w: %s", ErrProviderNotFound, name)
	}

	if len(cachedRoutes) > 0 {
		for _, route := range cachedRoutes {
			route.Provider = spec.Name
			if err := subscriptions.Activate(providerCtx, route); err != nil && m.logger != nil {
				m.logger.Printf("provider/%s: reapply route %s: %v", spec.Name, route.Type, err)
			}
		}
		m.clearCachedRoutes(name)
	}

	m.logErrors(providerCtx, fmt.Sprintf("provider/%s", spec.Name), instance.Errors())
	if m.bus != nil {
		runtime := dispatcher.NewRuntime(m.bus, m.table, m.pools)
		errCh := runtime.Start(providerCtx, instance.Events())
		m.logErrors(providerCtx, fmt.Sprintf("dispatcher/%s", spec.Name), errCh)
	}
	return nil
}

func (m *Manager) prepareProviderStart(name string) (config.ProviderSpec, []dispatcher.Route, error) {
	m.mu.Lock()
	state, ok := m.states[name]
	if !ok {
		m.mu.Unlock()
		return config.ProviderSpec{}, nil, fmt.Errorf("%w: %s", ErrProviderNotFound, name)
	}
	if state.running {
		m.mu.Unlock()
		return config.ProviderSpec{}, nil, fmt.Errorf("%w: %s", ErrProviderRunning, name)
	}
	if state.status == StatusStarting {
		m.mu.Unlock()
		return config.ProviderSpec{}, nil, fmt.Errorf("%w: %s", ErrProviderStarting, name)
	}
	state.status = StatusStarting
	state.startupErr = nil
	spec := state.spec
	cachedRoutes := cloneRoutes(state.cachedRoutes)
	m.mu.Unlock()
	m.persistSnapshot(name)
	return spec, cachedRoutes, nil
}

func (m *Manager) recordProviderStartFailure(name string, startErr error) {
	m.mu.Lock()
	state, ok := m.states[name]
	if ok {
		state.cancel = nil
		state.instance = nil
		state.subscriptions = nil
		state.running = false
		state.status = StatusFailed
		state.startupErr = startErr
	}
	m.mu.Unlock()
	m.persistSnapshot(name)
}

func (m *Manager) recordProviderStartSuccess(name string, cancel context.CancelFunc, instance Instance, subscriptions *shared.SubscriptionManager) bool {
	m.mu.Lock()
	state, ok := m.states[name]
	if ok {
		state.cancel = cancel
		state.instance = instance
		state.subscriptions = subscriptions
		state.running = true
		state.status = StatusRunning
		state.startupErr = nil
		state.cachedInstruments = nil
	}
	m.mu.Unlock()
	if ok {
		m.persistSnapshot(name)
	}
	return ok
}

func (m *Manager) clearCachedRoutes(name string) {
	m.mu.Lock()
	if state, ok := m.states[name]; ok && state.running {
		state.cachedRoutes = nil
	}
	m.mu.Unlock()
}

func (m *Manager) stopProviderLocked(state *providerState) {
	if state == nil || !state.running {
		return
	}
	if state.instance != nil {
		if snapshot := state.instance.Instruments(); snapshot != nil {
			state.cachedInstruments = schema.CloneInstruments(snapshot)
		} else {
			state.cachedInstruments = nil
		}
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
	state.status = StatusStopped
	state.startupErr = nil
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

func (m *Manager) snapshotFor(name string) (providerstore.Snapshot, bool) {
	m.mu.RLock()
	state, ok := m.states[name]
	if !ok {
		m.mu.RUnlock()
		var empty providerstore.Snapshot
		return empty, false
	}
	snapshot := providerstore.Snapshot{
		Name:        state.spec.Name,
		DisplayName: state.spec.Name,
		Adapter:     state.spec.Adapter,
		Config:      cloneConfigMap(state.spec.Config),
		Status:      string(state.status),
		Metadata:    nil,
	}
	m.mu.RUnlock()
	return snapshot, true
}

func (m *Manager) persistSnapshot(name string) {
	if m.persistence == nil {
		return
	}
	snapshot, ok := m.snapshotFor(name)
	if !ok {
		return
	}
	ctx := m.parentContext()
	if err := m.persistence.SaveProvider(ctx, snapshot); err != nil && m.logger != nil {
		m.logger.Printf("provider/%s: persist state failed: %v", name, err)
	}
}

func (m *Manager) deleteSnapshot(name string) {
	if m.persistence == nil {
		return
	}
	ctx := m.parentContext()
	if err := m.persistence.DeleteProvider(ctx, name); err != nil && m.logger != nil {
		m.logger.Printf("provider/%s: delete persisted state failed: %v", name, err)
	}
}

// Restore primes the manager with a snapshot loaded from persistence without starting the provider.
func (m *Manager) Restore(snapshot providerstore.Snapshot) {
	name := strings.TrimSpace(snapshot.Name)
	if name == "" {
		return
	}
	spec := config.ProviderSpec{
		Name:    name,
		Adapter: strings.TrimSpace(snapshot.Adapter),
		Config:  cloneConfigMap(snapshot.Config),
	}
	status := statusFromString(snapshot.Status)
	m.mu.Lock()
	if _, exists := m.states[name]; exists {
		m.mu.Unlock()
		return
	}
	m.states[name] = &providerState{
		spec:              spec,
		instance:          nil,
		subscriptions:     nil,
		cancel:            nil,
		cachedRoutes:      nil,
		cachedInstruments: nil,
		running:           false,
		status:            normalizeRestoredStatus(status),
		startupErr:        nil,
	}
	m.mu.Unlock()
	m.loadRoutes(name)
}

func normalizeRestoredStatus(status Status) Status {
	switch status {
	case StatusPending:
		return StatusPending
	case StatusRunning:
		return StatusStopped
	case StatusStarting:
		return StatusStopped
	case StatusStopped:
		return StatusStopped
	case StatusFailed:
		return StatusFailed
	}
	return StatusPending
}

func (m *Manager) persistRoutes(name string) {
	if m.persistence == nil {
		return
	}
	routes, ok := m.routeSnapshots(name)
	if !ok {
		return
	}
	ctx := m.parentContext()
	if err := m.persistence.SaveRoutes(ctx, name, routes); err != nil && m.logger != nil {
		m.logger.Printf("provider/%s: persist routes failed: %v", name, err)
	}
}

func (m *Manager) deleteRoutes(name string) {
	if m.persistence == nil {
		return
	}
	ctx := m.parentContext()
	if err := m.persistence.DeleteRoutes(ctx, name); err != nil && m.logger != nil {
		m.logger.Printf("provider/%s: delete routes failed: %v", name, err)
	}
}

func (m *Manager) loadRoutes(name string) {
	if m.persistence == nil {
		return
	}
	ctx := m.parentContext()
	snapshots, err := m.persistence.LoadRoutes(ctx, name)
	if err != nil {
		if m.logger != nil {
			m.logger.Printf("provider/%s: load routes failed: %v", name, err)
		}
		return
	}
	if len(snapshots) == 0 {
		return
	}
	routes := dispatcherRoutesFromSnapshots(name, snapshots)
	m.mu.Lock()
	if state, ok := m.states[name]; ok {
		state.cachedRoutes = routes
	}
	m.mu.Unlock()
}

func (m *Manager) routeSnapshots(name string) ([]providerstore.RouteSnapshot, bool) {
	m.mu.RLock()
	state, ok := m.states[name]
	var routes []dispatcher.Route
	if ok {
		routes = cloneRoutes(state.cachedRoutes)
	}
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return routeSnapshotsFromRoutes(routes), true
}

func routeSnapshotsFromRoutes(routes []dispatcher.Route) []providerstore.RouteSnapshot {
	if len(routes) == 0 {
		return nil
	}
	out := make([]providerstore.RouteSnapshot, 0, len(routes))
	for _, route := range routes {
		snapshot := providerstore.RouteSnapshot{
			Type:     route.Type,
			WSTopics: append([]string(nil), route.WSTopics...),
			RestFns:  make([]providerstore.RouteRestFn, len(route.RestFns)),
			Filters:  make([]providerstore.RouteFilter, len(route.Filters)),
		}
		for i, fn := range route.RestFns {
			snapshot.RestFns[i] = providerstore.RouteRestFn{
				Name:     fn.Name,
				Endpoint: fn.Endpoint,
				Interval: fn.Interval,
				Parser:   fn.Parser,
			}
		}
		for i, filter := range route.Filters {
			snapshot.Filters[i] = providerstore.RouteFilter{
				Field: filter.Field,
				Op:    filter.Op,
				Value: filter.Value,
			}
		}
		out = append(out, snapshot)
	}
	return out
}

func dispatcherRoutesFromSnapshots(provider string, snapshots []providerstore.RouteSnapshot) []dispatcher.Route {
	if len(snapshots) == 0 {
		return nil
	}
	out := make([]dispatcher.Route, 0, len(snapshots))
	for _, snapshot := range snapshots {
		route := dispatcher.Route{
			Provider: provider,
			Type:     schema.NormalizeRouteType(snapshot.Type),
			WSTopics: append([]string(nil), snapshot.WSTopics...),
			RestFns:  make([]dispatcher.RestFn, len(snapshot.RestFns)),
			Filters:  make([]dispatcher.FilterRule, len(snapshot.Filters)),
		}
		for i, fn := range snapshot.RestFns {
			route.RestFns[i] = dispatcher.RestFn{
				Name:     fn.Name,
				Endpoint: fn.Endpoint,
				Interval: fn.Interval,
				Parser:   fn.Parser,
			}
		}
		for i, filter := range snapshot.Filters {
			route.Filters[i] = dispatcher.FilterRule{
				Field: filter.Field,
				Op:    filter.Op,
				Value: filter.Value,
			}
		}
		out = append(out, route)
	}
	return out
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
		runtime := buildRuntimeMetadata(state.spec, instrumentCount, state.running, state.status, state.startupErr)
		out = append(out, runtime)
	}
	m.mu.RUnlock()
	SortRuntimeMetadata(out)
	return out
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
	var status Status
	var startupErr error
	var cachedInstruments []schema.Instrument
	if ok {
		spec = state.spec
		instance = state.instance
		running = state.running && instance != nil
		status = state.status
		startupErr = state.startupErr
		if len(state.cachedInstruments) > 0 {
			cachedInstruments = schema.CloneInstruments(state.cachedInstruments)
		}
	}
	m.mu.RUnlock()
	if !ok {
		m.recordCacheMiss(trimmed, providerMetadataCacheName)
		var empty RuntimeDetail
		return empty, false
	}
	instruments := make([]schema.Instrument, 0)
	instrumentCount := 0
	if running {
		if instInstruments := instance.Instruments(); instInstruments != nil {
			instruments = instInstruments
		}
		instrumentCount = len(instruments)
	} else if len(cachedInstruments) > 0 {
		instruments = cachedInstruments
		instrumentCount = len(instruments)
	}
	meta := buildRuntimeMetadata(spec, instrumentCount, running, status, startupErr)
	adapterMeta, _ := m.registry.AdapterMetadata(spec.Adapter)
	detail := RuntimeDetail{
		RuntimeMetadata: meta,
		Instruments:     instruments,
		AdapterMetadata: adapterMeta,
	}
	m.recordCacheHit(trimmed, providerMetadataCacheName)
	return CloneRuntimeDetail(detail), true
}

func buildRuntimeMetadata(spec config.ProviderSpec, instrumentCount int, running bool, status Status, startupErr error) RuntimeMetadata {
	settings := extractProviderSettings(spec.Config)
	errMsg := ""
	if startupErr != nil {
		errMsg = startupErr.Error()
	}
	meta := RuntimeMetadata{
		Name:                   spec.Name,
		Adapter:                spec.Adapter,
		Identifier:             spec.Adapter,
		Settings:               settings,
		InstrumentCount:        instrumentCount,
		Running:                running,
		Status:                 status,
		StartupError:           errMsg,
		DependentInstances:     nil,
		DependentInstanceCount: 0,
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

func (m *Manager) initCacheMetrics() {
	meter := otel.Meter("provider.manager.cache")
	if counter, err := meter.Int64Counter("meltica_provider_cache_hits",
		metric.WithDescription("Provider cache hits by cache name"),
		metric.WithUnit("{request}")); err == nil {
		m.cacheHitCounter = counter
	}
	if counter, err := meter.Int64Counter("meltica_provider_cache_misses",
		metric.WithDescription("Provider cache misses by cache name"),
		metric.WithUnit("{request}")); err == nil {
		m.cacheMissCounter = counter
	}
}

func (m *Manager) recordCacheHit(provider, cache string) {
	m.recordCacheMetric(m.cacheHitCounter, provider, cache)
}

func (m *Manager) recordCacheMiss(provider, cache string) {
	m.recordCacheMetric(m.cacheMissCounter, provider, cache)
}

func (m *Manager) recordCacheMetric(counter metric.Int64Counter, provider, cache string) {
	if counter == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("environment", telemetry.Environment()),
		attribute.String("cache", cache),
	}
	trimmed := strings.TrimSpace(provider)
	if trimmed != "" {
		attrs = append(attrs, attribute.String("provider", trimmed))
	}
	counter.Add(context.Background(), 1, metric.WithAttributes(attrs...))
}

// SanitizedProviderSpecs returns provider specifications with sensitive fields removed.
func (m *Manager) SanitizedProviderSpecs() []config.ProviderSpec {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.states) == 0 {
		return nil
	}
	specs := make([]config.ProviderSpec, 0, len(m.states))
	for _, state := range m.states {
		specs = append(specs, SanitizeProviderSpec(state.spec))
	}
	sort.Slice(specs, func(i, j int) bool {
		return specs[i].Name < specs[j].Name
	})
	return specs
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
