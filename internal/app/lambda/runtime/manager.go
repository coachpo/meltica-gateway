// Package runtime manages lambda lifecycle orchestration and strategy execution.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/app/dispatcher"
	"github.com/coachpo/meltica/internal/app/lambda/core"
	"github.com/coachpo/meltica/internal/app/lambda/js"
	"github.com/coachpo/meltica/internal/app/lambda/strategies"
	"github.com/coachpo/meltica/internal/app/provider"
	"github.com/coachpo/meltica/internal/app/risk"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/bus/eventbus"
	"github.com/coachpo/meltica/internal/infra/config"
	"github.com/coachpo/meltica/internal/infra/pool"
)

var (
	// ErrInstanceExists is returned when attempting to create an instance that already exists.
	ErrInstanceExists = errors.New("strategy instance already exists")
	// ErrInstanceNotFound is returned when attempting to access an instance that doesn't exist.
	ErrInstanceNotFound = errors.New("strategy instance not found")
	// ErrInstanceAlreadyRunning is returned when attempting to start an already running instance.
	ErrInstanceAlreadyRunning = errors.New("strategy instance already running")
	// ErrInstanceNotRunning is returned when attempting to stop an instance that isn't running.
	ErrInstanceNotRunning = errors.New("strategy instance not running")
)

func buildRiskLimits(cfg config.RiskConfig, logger *log.Logger) risk.Limits {
	maxPosSize, _ := decimal.NewFromString(cfg.MaxPositionSize)
	maxNotional, _ := decimal.NewFromString(cfg.MaxNotionalValue)

	var allowedOrderTypes []schema.OrderType
	if len(cfg.AllowedOrderTypes) > 0 {
		allowedOrderTypes = make([]schema.OrderType, 0, len(cfg.AllowedOrderTypes))
		seen := make(map[string]struct{}, len(cfg.AllowedOrderTypes))
		for _, raw := range cfg.AllowedOrderTypes {
			trimmed := strings.TrimSpace(raw)
			if trimmed == "" {
				continue
			}
			key := strings.ToLower(trimmed)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			allowedOrderTypes = append(allowedOrderTypes, schema.OrderType(trimmed))
		}
	}

	var breakerCooldown time.Duration
	if cfg.CircuitBreaker.Cooldown != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(cfg.CircuitBreaker.Cooldown))
		if err != nil {
			if logger != nil {
				logger.Printf("risk: invalid circuit breaker cooldown %q: %v", cfg.CircuitBreaker.Cooldown, err)
			}
		} else {
			breakerCooldown = parsed
		}
	}

	return risk.Limits{
		MaxPositionSize:     maxPosSize,
		MaxNotionalValue:    maxNotional,
		NotionalCurrency:    cfg.NotionalCurrency,
		OrderThrottle:       cfg.OrderThrottle,
		OrderBurst:          cfg.OrderBurst,
		MaxConcurrentOrders: cfg.MaxConcurrentOrders,
		PriceBandPercent:    cfg.PriceBandPercent,
		AllowedOrderTypes:   allowedOrderTypes,
		KillSwitchEnabled:   cfg.KillSwitchEnabled,
		MaxRiskBreaches:     cfg.MaxRiskBreaches,
		CircuitBreaker: risk.CircuitBreaker{
			Enabled:   cfg.CircuitBreaker.Enabled,
			Threshold: cfg.CircuitBreaker.Threshold,
			Cooldown:  breakerCooldown,
		},
	}
}

// StrategyFactory creates trading strategy instances from configuration.
type StrategyFactory func(config map[string]any) (core.TradingStrategy, error)

// StrategyDefinition combines strategy metadata with a factory function.
type StrategyDefinition struct {
	meta    strategies.Metadata
	factory StrategyFactory
}

// Metadata returns the strategy metadata.
func (d StrategyDefinition) Metadata() strategies.Metadata {
	return strategies.CloneMetadata(d.meta)
}

// ProviderCatalog provides access to available providers.
type ProviderCatalog interface {
	Provider(name string) (provider.Instance, bool)
}

// RouteRegistrar manages dynamic route registration for providers.
type RouteRegistrar interface {
	RegisterLambda(ctx context.Context, lambdaID string, providers []string, routes []dispatcher.RouteDeclaration) error
	RegisterLambdaBatch(ctx context.Context, regs []dispatcher.LambdaBatchRegistration) error
	UnregisterLambda(ctx context.Context, lambdaID string) error
}

// Manager coordinates lambda lifecycle and strategy execution.
type Manager struct {
	mu sync.RWMutex

	lifecycleMu  sync.RWMutex
	lifecycleCtx context.Context

	bus         eventbus.Bus
	pools       *pool.PoolManager
	providers   ProviderCatalog
	logger      *log.Logger
	registrar   RouteRegistrar
	riskManager *risk.Manager
	jsLoader    *js.Loader
	dynamic     map[string]struct{}
	strategyDir string
	base        map[string]StrategyDefinition

	strategies map[string]StrategyDefinition
	specs      map[string]config.LambdaSpec
	instances  map[string]*lambdaInstance
}

type lambdaInstance struct {
	base   *core.BaseLambda
	cancel context.CancelFunc
	errs   <-chan error
	strat  core.TradingStrategy
}

// NewManager creates a new lambda manager with the specified dependencies.
func NewManager(cfg config.AppConfig, bus eventbus.Bus, pools *pool.PoolManager, providers ProviderCatalog, logger *log.Logger, registrar RouteRegistrar) (*Manager, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "lambda-manager ", log.LstdFlags|log.Lmicroseconds)
	}

	rm := risk.NewManager(buildRiskLimits(cfg.Risk, logger))

	dir := strings.TrimSpace(cfg.Strategies.Directory)
	if dir == "" {
		dir = "strategies"
	}

	loader, err := js.NewLoader(dir)
	if err != nil {
		return nil, fmt.Errorf("lambda manager: create loader: %w", err)
	}

	mgr := &Manager{
		mu:           sync.RWMutex{},
		lifecycleMu:  sync.RWMutex{},
		lifecycleCtx: context.Background(),
		bus:          bus,
		pools:        pools,
		providers:    providers,
		logger:       logger,
		registrar:    registrar,
		riskManager:  rm,
		jsLoader:     loader,
		dynamic:      make(map[string]struct{}),
		strategyDir:  loader.Root(),
		base:         make(map[string]StrategyDefinition),
		strategies:   make(map[string]StrategyDefinition),
		specs:        make(map[string]config.LambdaSpec),
		instances:    make(map[string]*lambdaInstance),
	}
	if _, err := mgr.installJavaScriptStrategies(context.Background()); err != nil {
		return nil, fmt.Errorf("lambda manager: install javascript strategies: %w", err)
	}
	return mgr, nil
}

// SetLifecycleContext configures the parent context used to run lambda instances.
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

// RiskLimits returns the currently applied risk limits.
func (m *Manager) RiskLimits() risk.Limits {
	return m.riskManager.Limits()
}

// UpdateRiskLimits applies new risk limits across strategy instances.
func (m *Manager) UpdateRiskLimits(limits risk.Limits) {
	m.riskManager.UpdateLimits(limits)
	if m.logger != nil {
		allowed := "none"
		if len(limits.AllowedOrderTypes) > 0 {
			names := make([]string, 0, len(limits.AllowedOrderTypes))
			for _, ot := range limits.AllowedOrderTypes {
				names = append(names, string(ot))
			}
			allowed = strings.Join(names, ",")
		}
		m.logger.Printf(
			"risk limits applied: throttle=%.2f burst=%d maxPosition=%s maxNotional=%s concurrent=%d killSwitch=%t priceBand=%.2f allowedTypes=%s circuitBreaker(enabled=%t threshold=%d cooldown=%s)",
			limits.OrderThrottle,
			limits.OrderBurst,
			limits.MaxPositionSize.String(),
			limits.MaxNotionalValue.String(),
			limits.MaxConcurrentOrders,
			limits.KillSwitchEnabled,
			limits.PriceBandPercent,
			allowed,
			limits.CircuitBreaker.Enabled,
			limits.CircuitBreaker.Threshold,
			limits.CircuitBreaker.Cooldown,
		)
	}
}

// ApplyRiskConfig converts the supplied risk configuration into limits and applies them.
func (m *Manager) ApplyRiskConfig(cfg config.RiskConfig) risk.Limits {
	limits := buildRiskLimits(cfg, m.logger)
	m.UpdateRiskLimits(limits)
	return limits
}

// RefreshJavaScriptStrategies reloads JavaScript modules and restarts affected instances.
func (m *Manager) RefreshJavaScriptStrategies(ctx context.Context) error {
	previous := m.currentDynamicSet()
	running := m.instanceIDsForStrategies(previous)

	newSet, err := m.installJavaScriptStrategies(ctx)
	if err != nil {
		return err
	}
	m.stopInstances(running)
	if newSet == nil {
		newSet = map[string]struct{}{}
	}
	m.restartInstances(ctx, running, newSet)
	return nil
}

func normalizeStrategyDefinition(def StrategyDefinition) (StrategyDefinition, error) {
	name := strings.ToLower(strings.TrimSpace(def.meta.Name))
	if name == "" {
		return StrategyDefinition{}, fmt.Errorf("strategy name required")
	}
	if def.factory == nil {
		return StrategyDefinition{}, fmt.Errorf("strategy %s missing factory", name)
	}
	def.meta.Name = name

	if len(def.meta.Events) == 0 {
		strat, err := def.factory(map[string]any{})
		if err == nil && strat != nil {
			def.meta.Events = append([]schema.EventType(nil), strat.SubscribedEvents()...)
			closeStrategy(strat)
		}
	}
	def.meta.Events = append([]schema.EventType(nil), def.meta.Events...)

	fields := make([]strategies.ConfigField, len(def.meta.Config))
	copy(fields, def.meta.Config)
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	def.meta.Config = fields

	return def, nil
}

func (m *Manager) installJavaScriptStrategies(ctx context.Context) (map[string]struct{}, error) {
	if m.jsLoader == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := m.jsLoader.Refresh(ctx); err != nil {
		return nil, fmt.Errorf("load javascript strategies: %w", err)
	}

	summaries := m.jsLoader.List()
	definitions := make(map[string]StrategyDefinition, len(summaries))
	for _, summary := range summaries {
		module, err := m.jsLoader.Get(summary.Name)
		if err != nil {
			return nil, fmt.Errorf("load strategy %s: %w", summary.Name, err)
		}
		mod := module
		def := StrategyDefinition{
			meta: strategies.CloneMetadata(summary.Metadata),
			factory: func(cfg map[string]any) (core.TradingStrategy, error) {
				return js.NewStrategy(mod, cfg, m.logger)
			},
		}
		normalized, err := normalizeStrategyDefinition(def)
		if err != nil {
			return nil, fmt.Errorf("strategy %s: %w", summary.Name, err)
		}
		definitions[normalized.meta.Name] = normalized
	}

	m.mu.Lock()
	for name := range m.dynamic {
		delete(m.strategies, name)
		if baseDef, ok := m.base[name]; ok {
			m.strategies[name] = baseDef
		}
	}
	m.dynamic = make(map[string]struct{}, len(definitions))
	for name, def := range definitions {
		m.strategies[name] = def
		m.dynamic[name] = struct{}{}
	}
	m.mu.Unlock()

	return copyStringSet(m.dynamic), nil
}

func (m *Manager) currentDynamicSet() map[string]struct{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return copyStringSet(m.dynamic)
}

func (m *Manager) instanceIDsForStrategies(names map[string]struct{}) []string {
	if len(names) == 0 {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0)
	for id := range m.instances {
		spec, ok := m.specs[id]
		if !ok {
			continue
		}
		strategyName := strings.ToLower(strings.TrimSpace(spec.Strategy.Identifier))
		if _, ok := names[strategyName]; ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func (m *Manager) stopInstances(ids []string) {
	for _, id := range ids {
		if err := m.Stop(id); err != nil && !errors.Is(err, ErrInstanceNotRunning) {
			if m.logger != nil {
				m.logger.Printf("stop strategy %s: %v", id, err)
			}
		}
	}
}

func (m *Manager) restartInstances(ctx context.Context, ids []string, available map[string]struct{}) {
	if ctx == nil {
		ctx = context.Background()
	}
	for _, id := range ids {
		spec, err := m.specForID(id)
		if err != nil {
			if m.logger != nil {
				m.logger.Printf("restart strategy %s: spec lookup failed: %v", id, err)
			}
			continue
		}
		name := strings.ToLower(strings.TrimSpace(spec.Strategy.Identifier))
		if _, ok := available[name]; !ok {
			continue
		}
		if err := m.Start(ctx, id); err != nil {
			if m.logger != nil && !errors.Is(err, ErrInstanceAlreadyRunning) {
				m.logger.Printf("restart strategy %s: %v", id, err)
			}
		}
	}
}

// StrategyCatalog returns all available strategy metadata.
func (m *Manager) StrategyCatalog() []strategies.Metadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]strategies.Metadata, 0, len(m.strategies))
	for _, def := range m.strategies {
		out = append(out, def.Metadata())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// StrategyDetail returns metadata for a specific strategy by name.
func (m *Manager) StrategyDetail(name string) (strategies.Metadata, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	def, ok := m.strategies[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		var empty strategies.Metadata
		return empty, false
	}
	return def.Metadata(), true
}

// StartFromManifest registers all lambdas defined in the lambda manifest without starting them.
func (m *Manager) StartFromManifest(manifest config.LambdaManifest) error {
	for _, definition := range manifest.Lambdas {
		spec := sanitizeSpec(definition)
		if err := m.ensureSpec(spec, false); err != nil {
			return err
		}
	}

	return nil
}

// Create creates a new lambda instance from the specification.
func (m *Manager) Create(spec config.LambdaSpec) (*core.BaseLambda, error) {
	spec = sanitizeSpec(spec)
	if spec.ID == "" || len(spec.Providers) == 0 || spec.Strategy.Identifier == "" {
		return nil, fmt.Errorf("strategy instance requires id, providers, and strategy")
	}
	if len(spec.AllSymbols()) == 0 {
		return nil, fmt.Errorf("strategy %s: instrument symbols required", spec.ID)
	}
	if err := m.ensureSpec(spec, false); err != nil {
		return nil, err
	}
	return nil, nil
}

func (m *Manager) ensureSpec(spec config.LambdaSpec, allowReplace bool) error {
	if spec.Strategy.Config == nil {
		spec.Strategy.Config = make(map[string]any)
	}
	name := strings.ToLower(strings.TrimSpace(spec.Strategy.Identifier))
	if _, ok := m.strategies[name]; !ok {
		return fmt.Errorf("strategy %q not registered", spec.Strategy.Identifier)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.specs[spec.ID]; exists && !allowReplace {
		return ErrInstanceExists
	}
	m.specs[spec.ID] = cloneSpec(spec)
	return nil
}

// Start starts a lambda instance by ID.
func (m *Manager) Start(ctx context.Context, id string) error {
	spec, err := m.specForID(id)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if _, running := m.instances[spec.ID]; running {
		m.mu.Unlock()
		return ErrInstanceAlreadyRunning
	}
	m.mu.Unlock()

	_, _, _, err = m.launch(ctx, spec, true)
	return err
}

func (m *Manager) launch(ctx context.Context, spec config.LambdaSpec, registerNow bool) (*core.BaseLambda, []string, []dispatcher.RouteDeclaration, error) {
	providers := spec.Providers
	if len(providers) == 0 {
		return nil, nil, nil, fmt.Errorf("strategy %s: providers required", spec.ID)
	}
	resolvedProviders := make([]string, 0, len(providers))
	for _, name := range providers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := m.providers.Provider(name); !ok {
			return nil, nil, nil, fmt.Errorf("provider %q unavailable", name)
		}
		resolvedProviders = append(resolvedProviders, name)
	}
	if len(resolvedProviders) == 0 {
		return nil, nil, nil, fmt.Errorf("strategy %s: no valid providers resolved", spec.ID)
	}

	strategy, err := m.buildStrategy(spec.Strategy.Identifier, spec.Strategy.Config)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("strategy %s: %w", spec.ID, err)
	}
	if strategy != nil && len(resolvedProviders) > 1 && !strategy.WantsCrossProviderEvents() {
		return nil, nil, nil, fmt.Errorf("strategy %s does not support cross-provider feeds", spec.Strategy.Identifier)
	}

	routes := buildRouteDeclarations(strategy, spec)
	var registered bool
	if registerNow && m.registrar != nil && len(routes) > 0 {
		if err := m.registrar.RegisterLambda(ctx, spec.ID, resolvedProviders, routes); err != nil {
			return nil, nil, nil, fmt.Errorf("strategy %s: register routes: %w", spec.ID, err)
		}
		registered = true
	}

	orderRouter := &providerOrderRouter{catalog: m.providers}
	dryRun := true
	if raw, ok := spec.Strategy.Config["dry_run"]; ok {
		if val, ok := raw.(bool); ok {
			dryRun = val
		}
	}
	baseCfg := core.Config{Providers: resolvedProviders, ProviderSymbols: spec.ProviderSymbolMap(), DryRun: dryRun}
	base := core.NewBaseLambda(spec.ID, baseCfg, m.bus, orderRouter, m.pools, strategy, m.riskManager)
	bindStrategy(strategy, base, m.logger)

	runCtx, cancel := context.WithCancel(m.parentContext())
	errs, err := base.Start(runCtx)
	if err != nil {
		cancel()
		if registered && m.registrar != nil {
			_ = m.registrar.UnregisterLambda(ctx, spec.ID)
		}
		return nil, nil, nil, fmt.Errorf("start strategy %s: %w", spec.ID, err)
	}

	m.mu.Lock()
	m.instances[spec.ID] = &lambdaInstance{base: base, cancel: cancel, errs: errs, strat: strategy}
	m.mu.Unlock()

	go m.observe(runCtx, spec.ID, errs, strategy)
	return base, resolvedProviders, routes, nil
}

func (m *Manager) specForID(id string) (config.LambdaSpec, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return config.LambdaSpec{}, ErrInstanceNotFound
	}
	m.mu.RLock()
	spec, ok := m.specs[id]
	m.mu.RUnlock()
	if !ok {
		return config.LambdaSpec{}, ErrInstanceNotFound
	}
	return cloneSpec(spec), nil
}

// Stop stops a running lambda instance by ID.
func (m *Manager) Stop(id string) error {
	id = strings.TrimSpace(id)
	m.mu.Lock()
	inst, running := m.instances[id]
	if !running {
		if _, exists := m.specs[id]; !exists {
			m.mu.Unlock()
			return ErrInstanceNotFound
		}
		m.mu.Unlock()
		return ErrInstanceNotRunning
	}
	delete(m.instances, id)
	m.mu.Unlock()

	inst.cancel()
	if m.registrar != nil {
		_ = m.registrar.UnregisterLambda(context.Background(), id)
	}
	closeStrategy(inst.strat)
	return nil
}

// Remove removes a lambda instance by ID after stopping it.
func (m *Manager) Remove(id string) error {
	err := m.Stop(id)
	if err != nil && !errors.Is(err, ErrInstanceNotRunning) {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.specs[id]; !ok {
		return ErrInstanceNotFound
	}
	delete(m.specs, id)
	return nil
}

// Update updates an existing lambda instance with new configuration.
func (m *Manager) Update(ctx context.Context, spec config.LambdaSpec) error {
	spec = sanitizeSpec(spec)
	if spec.ID == "" {
		return ErrInstanceNotFound
	}

	m.mu.RLock()
	current, ok := m.specs[spec.ID]
	_, wasRunning := m.instances[spec.ID]
	m.mu.RUnlock()
	if !ok {
		return ErrInstanceNotFound
	}
	if !equalStringSlices(current.Providers, spec.Providers) {
		return fmt.Errorf("providers are immutable for %s", spec.ID)
	}
	if !equalProviderSymbols(current.ProviderSymbols, spec.ProviderSymbols) {
		return fmt.Errorf("scope assignments are immutable for %s", spec.ID)
	}
	if current.Strategy.Identifier != spec.Strategy.Identifier {
		return fmt.Errorf("strategy is immutable for %s", spec.ID)
	}
	if err := m.ensureSpec(spec, true); err != nil {
		return err
	}

	if err := m.Stop(spec.ID); err != nil && !errors.Is(err, ErrInstanceNotRunning) {
		return err
	}
	startAfterUpdate := wasRunning
	if startAfterUpdate {
		if _, _, _, err := m.launch(ctx, spec, true); err != nil {
			return err
		}
	}
	return nil
}

// InstanceSummary provides a flattened overview of a lambda instance.
type InstanceSummary struct {
	ID                 string   `json:"id"`
	StrategyIdentifier string   `json:"strategyIdentifier"`
	Providers          []string `json:"providers"`
	AggregatedSymbols  []string `json:"aggregatedSymbols"`
	Running            bool     `json:"running"`
}

// InstanceSnapshot captures the detailed state of a lambda instance.
type InstanceSnapshot struct {
	ID                string                            `json:"id"`
	Strategy          config.LambdaStrategySpec         `json:"strategy"`
	Providers         []string                          `json:"providers"`
	ProviderSymbols   map[string]config.ProviderSymbols `json:"scope"`
	AggregatedSymbols []string                          `json:"aggregatedSymbols"`
	Running           bool                              `json:"running"`
}

// Instances returns summaries of all lambda instances.
func (m *Manager) Instances() []InstanceSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]InstanceSummary, 0, len(m.specs))
	for id, spec := range m.specs {
		_, running := m.instances[id]
		out = append(out, summaryOf(spec, running))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Instance returns a snapshot of a specific lambda instance by ID.
func (m *Manager) Instance(id string) (InstanceSnapshot, bool) {
	spec, err := m.specForID(id)
	if err != nil {
		return InstanceSnapshot{
			ID:                "",
			Strategy:          config.LambdaStrategySpec{Identifier: "", Config: map[string]any{}},
			Providers:         []string{},
			ProviderSymbols:   map[string]config.ProviderSymbols{},
			AggregatedSymbols: []string{},
			Running:           false,
		}, false
	}
	m.mu.RLock()
	_, running := m.instances[spec.ID]
	m.mu.RUnlock()
	return snapshotOf(spec, running), true
}

func summaryOf(spec config.LambdaSpec, running bool) InstanceSummary {
	providers := append([]string(nil), spec.Providers...)
	aggregated := spec.AllSymbols()
	return InstanceSummary{
		ID:                 spec.ID,
		StrategyIdentifier: spec.Strategy.Identifier,
		Providers:          providers,
		AggregatedSymbols:  aggregated,
		Running:            running,
	}
}

func snapshotOf(spec config.LambdaSpec, running bool) InstanceSnapshot {
	strategyConfig := copyMap(spec.Strategy.Config)
	providers := append([]string(nil), spec.Providers...)
	assignments := cloneProviderSymbols(spec.ProviderSymbols)
	aggregated := spec.AllSymbols()
	return InstanceSnapshot{
		ID:                spec.ID,
		Strategy:          config.LambdaStrategySpec{Identifier: spec.Strategy.Identifier, Config: strategyConfig},
		Providers:         providers,
		ProviderSymbols:   assignments,
		AggregatedSymbols: aggregated,
		Running:           running,
	}
}

func (m *Manager) observe(ctx context.Context, id string, errs <-chan error, strat core.TradingStrategy) {
	defer closeStrategy(strat)
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-errs:
			if !ok {
				return
			}
			if err != nil {
				m.logger.Printf("strategy %s: %v", id, err)
			}
		}
	}
}

type providerOrderRouter struct {
	catalog ProviderCatalog
}

func (r *providerOrderRouter) SubmitOrder(ctx context.Context, req schema.OrderRequest) error {
	if r == nil || r.catalog == nil {
		return fmt.Errorf("order router not configured")
	}
	providerName := strings.TrimSpace(req.Provider)
	if providerName == "" {
		return fmt.Errorf("order provider required")
	}
	inst, ok := r.catalog.Provider(providerName)
	if !ok {
		return fmt.Errorf("provider %q unavailable", providerName)
	}
	if err := inst.SubmitOrder(ctx, req); err != nil {
		return fmt.Errorf("submit order to provider %q: %w", providerName, err)
	}
	return nil
}

func closeStrategy(strat core.TradingStrategy) {
	if strat == nil {
		return
	}
	type closer interface {
		Close()
	}
	switch s := strat.(type) {
	case closer:
		s.Close()
	case io.Closer:
		_ = s.Close()
	}
}

func (m *Manager) buildStrategy(name string, cfg map[string]any) (core.TradingStrategy, error) {
	def, ok := m.strategies[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil, fmt.Errorf("strategy %q not registered", name)
	}
	return def.factory(copyMap(cfg))
}

func sanitizeSpec(spec config.LambdaSpec) config.LambdaSpec {
	spec.ID = strings.TrimSpace(spec.ID)
	spec.Strategy.Normalize()
	spec.RefreshProviders()
	spec.Providers = normalizeProviderList(spec.Providers)
	if spec.ProviderSymbols == nil {
		spec.ProviderSymbols = make(map[string]config.ProviderSymbols)
	}
	return spec
}

func cloneSpec(spec config.LambdaSpec) config.LambdaSpec {
	clone := spec
	clone.Strategy.Config = copyMap(spec.Strategy.Config)
	clone.Providers = append([]string(nil), spec.Providers...)
	clone.ProviderSymbols = cloneProviderSymbols(spec.ProviderSymbols)
	return clone
}

func cloneProviderSymbols(src map[string]config.ProviderSymbols) map[string]config.ProviderSymbols {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]config.ProviderSymbols, len(src))
	for name, assignment := range src {
		cloned := config.ProviderSymbols{
			Symbols: append([]string(nil), assignment.Symbols...),
		}
		dst[name] = cloned
	}
	return dst
}

func copyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func copyStringSet(src map[string]struct{}) map[string]struct{} {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]struct{}, len(src))
	for key := range src {
		dst[key] = struct{}{}
	}
	return dst
}

// StrategyModules returns metadata for the currently loaded JavaScript strategy modules.
func (m *Manager) StrategyModules() []js.ModuleSummary {
	if m == nil || m.jsLoader == nil {
		return nil
	}
	return m.jsLoader.List()
}

// StrategyModule returns module metadata for a specific strategy.
func (m *Manager) StrategyModule(name string) (js.ModuleSummary, error) {
	if m == nil || m.jsLoader == nil {
		return js.ModuleSummary{}, js.ErrModuleNotFound
	}
	summary, err := m.jsLoader.Module(name)
	if err != nil {
		return js.ModuleSummary{}, fmt.Errorf("strategy module %q: %w", name, err)
	}
	return summary, nil
}

// StrategySource retrieves the raw JavaScript source for the named strategy.
func (m *Manager) StrategySource(name string) ([]byte, error) {
	if m == nil || m.jsLoader == nil {
		return nil, js.ErrModuleNotFound
	}
	source, err := m.jsLoader.Read(name)
	if err != nil {
		return nil, fmt.Errorf("strategy source %q: %w", name, err)
	}
	return source, nil
}

// UpsertStrategy writes or replaces a JavaScript strategy file.
func (m *Manager) UpsertStrategy(filename string, source []byte) error {
	if m == nil || m.jsLoader == nil {
		return fmt.Errorf("strategy loader unavailable")
	}
	if err := m.jsLoader.Write(filename, source); err != nil {
		return fmt.Errorf("strategy upsert %q: %w", filename, err)
	}
	return nil
}

// RemoveStrategy deletes the JavaScript strategy file by name.
func (m *Manager) RemoveStrategy(name string) error {
	if m == nil || m.jsLoader == nil {
		return js.ErrModuleNotFound
	}
	if err := m.jsLoader.Delete(name); err != nil {
		return fmt.Errorf("strategy remove %q: %w", name, err)
	}
	return nil
}

// StrategyDirectory returns the filesystem directory backing JavaScript strategies.
func (m *Manager) StrategyDirectory() string {
	if m == nil {
		return ""
	}
	return m.strategyDir
}

func providerInstrumentField(provider string) string {
	trimmed := strings.TrimSpace(provider)
	if trimmed == "" {
		return "instrument"
	}
	return "instrument@" + strings.ToLower(trimmed)
}

func normalizeProviderList(providers []string) []string {
	if len(providers) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(providers))
	out := make([]string, 0, len(providers))
	for _, raw := range providers {
		candidate := strings.TrimSpace(raw)
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalProviderSymbols(a, b map[string]config.ProviderSymbols) bool {
	if len(a) != len(b) {
		return false
	}
	for name, assignmentA := range a {
		assignmentB, ok := b[name]
		if !ok {
			return false
		}
		if len(assignmentA.Symbols) != len(assignmentB.Symbols) {
			return false
		}
		for i := range assignmentA.Symbols {
			if assignmentA.Symbols[i] != assignmentB.Symbols[i] {
				return false
			}
		}
	}
	return true
}

func buildRouteDeclarations(strategy core.TradingStrategy, spec config.LambdaSpec) []dispatcher.RouteDeclaration {
	if strategy == nil {
		return nil
	}
	events := strategy.SubscribedEvents()
	if len(events) == 0 {
		return nil
	}
	routes := make([]dispatcher.RouteDeclaration, 0, len(events))
	providerSymbols := spec.ProviderSymbolMap()
	allSymbols := spec.AllSymbols()
	baseCurrency := ""
	quoteCurrency := ""
	if len(allSymbols) == 1 {
		if base, quote, err := schema.InstrumentCurrencies(allSymbols[0]); err == nil {
			baseCurrency = base
			quoteCurrency = quote
		}
	}
	baseCurrency = strings.ToUpper(strings.TrimSpace(baseCurrency))
	quoteCurrency = strings.ToUpper(strings.TrimSpace(quoteCurrency))

	seenCurrencies := make(map[string]struct{}, 2)
	seenRoutes := make(map[schema.RouteType]struct{})
	for _, evtType := range events {
		routesForEvent := schema.RoutesForEvent(evtType)
		for _, routeName := range routesForEvent {
			routeName = schema.NormalizeRouteType(routeName)
			if err := routeName.Validate(); err != nil {
				continue
			}
			if routeName == schema.RouteTypeAccountBalance {
				candidates := []string{baseCurrency, quoteCurrency}
				for _, currency := range candidates {
					currency = strings.ToUpper(strings.TrimSpace(currency))
					if currency == "" {
						continue
					}
					if _, ok := seenCurrencies[currency]; ok {
						continue
					}
					seenCurrencies[currency] = struct{}{}
					routeFilters := map[string]any{"currency": currency}
					routes = append(routes, dispatcher.RouteDeclaration{
						Type:    routeName,
						Filters: copyMap(routeFilters),
					})
				}
				continue
			}
			if _, ok := seenRoutes[routeName]; ok {
				continue
			}
			seenRoutes[routeName] = struct{}{}
			routeFilters := make(map[string]any)
			if len(allSymbols) > 0 {
				routeFilters["instrument"] = allSymbols
			}
			for provider, symbols := range providerSymbols {
				if len(symbols) == 0 {
					continue
				}
				key := providerInstrumentField(provider)
				routeFilters[key] = symbols
			}
			routes = append(routes, dispatcher.RouteDeclaration{
				Type:    routeName,
				Filters: copyMap(routeFilters),
			})
		}
	}
	return routes
}

func bindStrategy(strategy core.TradingStrategy, base *core.BaseLambda, _ *log.Logger) {
	switch s := strategy.(type) {
	case *js.Strategy:
		s.Attach(base)
	}
}
