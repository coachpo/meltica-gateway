// Package runtime manages lambda lifecycle orchestration and strategy execution.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/app/dispatcher"
	"github.com/coachpo/meltica/internal/app/lambda/core"
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
		for _, raw := range cfg.AllowedOrderTypes {
			trimmed := strings.TrimSpace(raw)
			if trimmed == "" {
				continue
			}
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

	bus          eventbus.Bus
	pools        *pool.PoolManager
	providers    ProviderCatalog
	logger       *log.Logger
	registrar    RouteRegistrar
	riskManager  *risk.Manager
	runtimeStore *config.RuntimeStore

	strategies map[string]StrategyDefinition
	specs      map[string]config.LambdaSpec
	instances  map[string]*lambdaInstance
}

type lambdaInstance struct {
	base   *core.BaseLambda
	cancel context.CancelFunc
	errs   <-chan error
}

// NewManager creates a new lambda manager with the specified dependencies.
func NewManager(cfg config.AppConfig, store *config.RuntimeStore, bus eventbus.Bus, pools *pool.PoolManager, providers ProviderCatalog, logger *log.Logger, registrar RouteRegistrar) *Manager {
	if logger == nil {
		logger = log.New(os.Stdout, "lambda-manager ", log.LstdFlags|log.Lmicroseconds)
	}

	riskCfg := cfg.Runtime.Risk
	if store != nil {
		snapshot := store.Snapshot()
		riskCfg = snapshot.Risk
	}
	rm := risk.NewManager(buildRiskLimits(riskCfg, logger))

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
		runtimeStore: store,
		strategies:   make(map[string]StrategyDefinition),
		specs:        make(map[string]config.LambdaSpec),
		instances:    make(map[string]*lambdaInstance),
	}
	mgr.registerDefaults()
	return mgr
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

func (m *Manager) deriveLaunchContext(ctx context.Context) (context.Context, context.CancelFunc) {
	parent := m.parentContext()

	if ctx == nil {
		return context.WithCancel(parent)
	}

	if ctx == parent {
		return context.WithCancel(ctx)
	}

	runCtx, cancel := context.WithCancel(ctx)
	go func(parentCtx, run context.Context, cancelFunc context.CancelFunc) {
		select {
		case <-parentCtx.Done():
			cancelFunc()
		case <-run.Done():
		}
	}(parent, runCtx, cancel)
	return runCtx, cancel
}

func (m *Manager) registerDefaults() {
	m.registerStrategy(StrategyDefinition{
		meta: strategies.CloneMetadata(strategies.NoOpMetadata),
		factory: func(_ map[string]any) (core.TradingStrategy, error) {
			return &strategies.NoOp{}, nil
		},
	})

	m.registerStrategy(StrategyDefinition{
		meta: strategies.CloneMetadata(strategies.DelayMetadata),
		factory: func(cfg map[string]any) (core.TradingStrategy, error) {
			minDelay := durationValue(cfg, "min_delay", strategies.DefaultMinDelay)
			maxDelay := durationValue(cfg, "max_delay", strategies.DefaultMaxDelay)

			if minDelay < 0 || maxDelay < 0 {
				return nil, fmt.Errorf("delay: min_delay and max_delay must be non-negative")
			}
			if maxDelay < minDelay {
				return nil, fmt.Errorf("delay: max_delay must be greater than or equal to min_delay")
			}

			return &strategies.Delay{MinDelay: minDelay, MaxDelay: maxDelay}, nil
		},
	})

	m.registerStrategy(StrategyDefinition{
		meta: strategies.CloneMetadata(strategies.LoggingMetadata),
		factory: func(cfg map[string]any) (core.TradingStrategy, error) {
			return &strategies.Logging{
				Logger:       nil,
				LoggerPrefix: stringValue(cfg, "logger_prefix", "[Logging] "),
			}, nil
		},
	})

	m.registerStrategy(StrategyDefinition{
		meta: strategies.CloneMetadata(strategies.MomentumMetadata),
		factory: func(cfg map[string]any) (core.TradingStrategy, error) {
			return &strategies.Momentum{
				Lambda:            nil,
				LookbackPeriod:    intValue(cfg, "lookback_period", 20),
				MomentumThreshold: floatValue(cfg, "momentum_threshold", 0.5),
				OrderSize:         stringValue(cfg, "order_size", "1"),
				Cooldown:          durationValue(cfg, "cooldown", 5*time.Second),
				DryRun:            boolValue(cfg, "dry_run", true),
			}, nil
		},
	})

	m.registerStrategy(StrategyDefinition{
		meta: strategies.CloneMetadata(strategies.MeanReversionMetadata),
		factory: func(cfg map[string]any) (core.TradingStrategy, error) {
			return &strategies.MeanReversion{
				Lambda:             nil,
				WindowSize:         intValue(cfg, "window_size", 20),
				DeviationThreshold: floatValue(cfg, "deviation_threshold", 0.5),
				OrderSize:          stringValue(cfg, "order_size", "1"),
				DryRun:             boolValue(cfg, "dry_run", true),
			}, nil
		},
	})

	m.registerStrategy(StrategyDefinition{
		meta: strategies.CloneMetadata(strategies.GridMetadata),
		factory: func(cfg map[string]any) (core.TradingStrategy, error) {
			return &strategies.Grid{
				Lambda:      nil,
				GridLevels:  intValue(cfg, "grid_levels", 3),
				GridSpacing: floatValue(cfg, "grid_spacing", 0.5),
				OrderSize:   stringValue(cfg, "order_size", "1"),
				BasePrice:   floatValue(cfg, "base_price", 0),
				DryRun:      boolValue(cfg, "dry_run", true),
			}, nil
		},
	})

	m.registerStrategy(StrategyDefinition{
		meta: strategies.CloneMetadata(strategies.MarketMakingMetadata),
		factory: func(cfg map[string]any) (core.TradingStrategy, error) {
			maxOrders := intValue(cfg, "max_open_orders", 2)
			if maxOrders > int(^uint32(0)>>1) {
				maxOrders = int(^uint32(0) >> 1)
			}
			// #nosec G115 - bounds checked above
			return &strategies.MarketMaking{
				Lambda:        nil,
				SpreadBps:     floatValue(cfg, "spread_bps", 25),
				OrderSize:     stringValue(cfg, "order_size", "1"),
				MaxOpenOrders: int32(maxOrders),
				DryRun:        boolValue(cfg, "dry_run", true),
			}, nil
		},
	})
}

// RiskLimits returns the currently applied risk limits.
func (m *Manager) RiskLimits() risk.Limits {
	return m.riskManager.Limits()
}

// UpdateRiskLimits applies new risk limits across strategy instances.
func (m *Manager) UpdateRiskLimits(limits risk.Limits) {
	m.riskManager.UpdateLimits(limits)
	if m.logger != nil {
		m.logger.Printf("risk limits updated: throttle=%.2f, burst=%d", limits.OrderThrottle, limits.OrderBurst)
	}
}

// ManifestSnapshot returns the current lambda manifest snapshot including dynamically created instances.
func (m *Manager) ManifestSnapshot() config.LambdaManifest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.specs) == 0 {
		return config.LambdaManifest{Lambdas: nil}
	}
	manifest := config.LambdaManifest{
		Lambdas: make([]config.LambdaSpec, 0, len(m.specs)),
	}
	for _, spec := range m.specs {
		cloned := cloneSpec(spec)
		manifest.Lambdas = append(manifest.Lambdas, sanitizeSpec(cloned))
	}
	sort.Slice(manifest.Lambdas, func(i, j int) bool {
		return manifest.Lambdas[i].ID < manifest.Lambdas[j].ID
	})
	return manifest
}

// ApplyRuntimeConfig synchronises the manager with the supplied runtime configuration snapshot.
func (m *Manager) ApplyRuntimeConfig(cfg config.RuntimeConfig) error {
	limits := buildRiskLimits(cfg.Risk, m.logger)
	m.UpdateRiskLimits(limits)
	return nil
}

// ApplyRiskConfig converts the supplied risk configuration into limits and applies them.
func (m *Manager) ApplyRiskConfig(cfg config.RiskConfig) (risk.Limits, error) {
	effective := cfg
	if m.runtimeStore != nil {
		updated, err := m.runtimeStore.UpdateRisk(cfg)
		if err != nil {
			return risk.Limits{}, fmt.Errorf("update runtime risk: %w", err)
		}
		effective = updated
	}
	limits := buildRiskLimits(effective, m.logger)
	m.UpdateRiskLimits(limits)
	return limits, nil
}

func (m *Manager) registerStrategy(def StrategyDefinition) {
	name := strings.ToLower(strings.TrimSpace(def.meta.Name))
	if name == "" {
		panic("strategy name required")
	}
	if def.factory == nil {
		panic(fmt.Sprintf("strategy %s missing factory", name))
	}
	def.meta.Name = name

	if len(def.meta.Events) == 0 {
		strat, err := def.factory(map[string]any{})
		if err == nil && strat != nil {
			def.meta.Events = append([]schema.EventType(nil), strat.SubscribedEvents()...)
		}
	}
	def.meta.Events = append([]schema.EventType(nil), def.meta.Events...)

	fields := make([]strategies.ConfigField, len(def.meta.Config))
	copy(fields, def.meta.Config)
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	def.meta.Config = fields

	m.strategies[name] = def
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

// StartFromManifest starts all lambdas defined in the lambda manifest.
func (m *Manager) StartFromManifest(ctx context.Context, manifest config.LambdaManifest) error {
	autoStart := make([]config.LambdaSpec, 0, len(manifest.Lambdas))
	for _, definition := range manifest.Lambdas {
		spec := sanitizeSpec(definition)
		if err := m.ensureSpec(spec, false); err != nil {
			return err
		}
		if spec.AutoStart {
			autoStart = append(autoStart, spec)
		}
	}

	if len(autoStart) == 0 {
		return nil
	}

	registerImmediately := m.registrar == nil
	started := make([]string, 0, len(autoStart))
	batch := make([]dispatcher.LambdaBatchRegistration, 0, len(autoStart))

	for _, spec := range autoStart {
		_, providers, routes, err := m.launch(ctx, spec, registerImmediately)
		if err != nil {
			for _, id := range started {
				_ = m.Stop(id)
			}
			return err
		}
		started = append(started, spec.ID)
		if !registerImmediately && len(routes) > 0 {
			entry := dispatcher.LambdaBatchRegistration{
				ID:        spec.ID,
				Providers: providers,
				Routes:    routes,
			}
			batch = append(batch, entry)
		}
	}

	if !registerImmediately && len(batch) > 0 {
		if err := m.registrar.RegisterLambdaBatch(ctx, batch); err != nil {
			for _, id := range started {
				_ = m.Stop(id)
			}
			return fmt.Errorf("register lambda batch: %w", err)
		}
	}
	return nil
}

// Create creates a new lambda instance from the specification.
func (m *Manager) Create(ctx context.Context, spec config.LambdaSpec) (*core.BaseLambda, error) {
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
	if err := m.Start(ctx, spec.ID); err != nil {
		return nil, err
	}
	m.mu.RLock()
	inst := m.instances[spec.ID]
	m.mu.RUnlock()
	if inst == nil {
		return nil, nil
	}
	return inst.base, nil
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
	dryRun := boolValue(spec.Strategy.Config, "dry_run", true)
	baseCfg := core.Config{Providers: resolvedProviders, ProviderSymbols: spec.ProviderSymbolMap(), DryRun: dryRun}
	base := core.NewBaseLambda(spec.ID, baseCfg, m.bus, orderRouter, m.pools, strategy, m.riskManager)
	bindStrategy(strategy, base, m.logger)

	runCtx, cancel := m.deriveLaunchContext(ctx)
	errs, err := base.Start(runCtx)
	if err != nil {
		cancel()
		if registered && m.registrar != nil {
			_ = m.registrar.UnregisterLambda(ctx, spec.ID)
		}
		return nil, nil, nil, fmt.Errorf("start strategy %s: %w", spec.ID, err)
	}

	m.mu.Lock()
	m.instances[spec.ID] = &lambdaInstance{base: base, cancel: cancel, errs: errs}
	m.mu.Unlock()

	go m.observe(runCtx, spec.ID, errs)
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

	spec.AutoStart = current.AutoStart
	if err := m.ensureSpec(spec, true); err != nil {
		return err
	}

	if err := m.Stop(spec.ID); err != nil && !errors.Is(err, ErrInstanceNotRunning) {
		return err
	}
	if _, _, _, err := m.launch(ctx, spec, true); err != nil {
		return err
	}
	return nil
}

// InstanceSummary provides a flattened overview of a lambda instance.
type InstanceSummary struct {
	ID                 string   `json:"id"`
	StrategyIdentifier string   `json:"strategyIdentifier"`
	Providers          []string `json:"providers"`
	AggregatedSymbols  []string `json:"aggregatedSymbols"`
	AutoStart          bool     `json:"autoStart"`
	Running            bool     `json:"running"`
}

// InstanceSnapshot captures the detailed state of a lambda instance.
type InstanceSnapshot struct {
	ID                string                            `json:"id"`
	Strategy          config.LambdaStrategySpec         `json:"strategy"`
	Providers         []string                          `json:"providers"`
	ProviderSymbols   map[string]config.ProviderSymbols `json:"scope"`
	AggregatedSymbols []string                          `json:"aggregatedSymbols"`
	AutoStart         bool                              `json:"autoStart"`
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
			AutoStart:         false,
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
		AutoStart:          spec.AutoStart,
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
		AutoStart:         spec.AutoStart,
		Running:           running,
	}
}

func (m *Manager) observe(ctx context.Context, id string, errs <-chan error) {
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
	case *strategies.Momentum:
		s.Lambda = &momentumAdapter{base: base}
	case *strategies.MeanReversion:
		s.Lambda = &orderStrategyAdapter{base: base}
	case *strategies.Grid:
		s.Lambda = &orderStrategyAdapter{base: base}
	case *strategies.MarketMaking:
		s.Lambda = &marketMakingAdapter{base: base}
	}
}

func stringValue(cfg map[string]any, key, def string) string {
	if cfg == nil {
		return def
	}
	if raw, ok := cfg[key]; ok {
		if val, ok := raw.(string); ok && strings.TrimSpace(val) != "" {
			return val
		}
	}
	return def
}

func boolValue(cfg map[string]any, key string, def bool) bool {
	if cfg == nil {
		return def
	}
	if raw, ok := cfg[key]; ok {
		switch v := raw.(type) {
		case bool:
			return v
		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				return def
			}
			if parsed, err := strconv.ParseBool(trimmed); err == nil {
				return parsed
			}
		case int:
			return v != 0
		case int64:
			return v != 0
		case float64:
			return v != 0
		}
	}
	return def
}

func intValue(cfg map[string]any, key string, def int) int {
	if cfg == nil {
		return def
	}
	if raw, ok := cfg[key]; ok {
		switch v := raw.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		case string:
			if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				return parsed
			}
		}
	}
	return def
}

func floatValue(cfg map[string]any, key string, def float64) float64 {
	if cfg == nil {
		return def
	}
	if raw, ok := cfg[key]; ok {
		switch v := raw.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case int64:
			return float64(v)
		case string:
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				return parsed
			}
		}
	}
	return def
}

func durationValue(cfg map[string]any, key string, def time.Duration) time.Duration {
	if cfg == nil {
		return def
	}
	if raw, ok := cfg[key]; ok {
		switch v := raw.(type) {
		case time.Duration:
			return v
		case string:
			if parsed, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
				return parsed
			}
		case int:
			return time.Duration(v) * time.Second
		case float64:
			return time.Duration(v * float64(time.Second))
		}
	}
	return def
}

func submitOrderWithFloat(ctx context.Context, base *core.BaseLambda, provider string, side schema.TradeSide, quantity string, price *float64) error {
	var priceStr *string
	if price != nil {
		formatted := strconv.FormatFloat(*price, 'f', -1, 64)
		priceStr = &formatted
	}
	if err := base.SubmitOrder(ctx, provider, side, quantity, priceStr); err != nil {
		return fmt.Errorf("submit order: %w", err)
	}
	return nil
}

type momentumAdapter struct {
	base *core.BaseLambda
}

func (a *momentumAdapter) Logger() *log.Logger   { return a.base.Logger() }
func (a *momentumAdapter) GetLastPrice() float64 { return a.base.GetLastPrice() }
func (a *momentumAdapter) IsTradingActive() bool { return a.base.IsTradingActive() }
func (a *momentumAdapter) Providers() []string   { return a.base.Providers() }
func (a *momentumAdapter) IsDryRun() bool        { return a.base.IsDryRun() }
func (a *momentumAdapter) SelectProvider(seed uint64) (string, error) {
	provider, err := a.base.SelectProvider(seed)
	if err != nil {
		return "", fmt.Errorf("select provider: %w", err)
	}
	return provider, nil
}
func (a *momentumAdapter) SubmitMarketOrder(ctx context.Context, provider string, side schema.TradeSide, quantity string) error {
	if err := a.base.SubmitMarketOrder(ctx, provider, side, quantity); err != nil {
		return fmt.Errorf("submit market order: %w", err)
	}
	return nil
}

type orderStrategyAdapter struct {
	base *core.BaseLambda
}

func (a *orderStrategyAdapter) Logger() *log.Logger   { return a.base.Logger() }
func (a *orderStrategyAdapter) GetLastPrice() float64 { return a.base.GetLastPrice() }
func (a *orderStrategyAdapter) IsTradingActive() bool { return a.base.IsTradingActive() }
func (a *orderStrategyAdapter) Providers() []string   { return a.base.Providers() }
func (a *orderStrategyAdapter) IsDryRun() bool        { return a.base.IsDryRun() }
func (a *orderStrategyAdapter) SelectProvider(seed uint64) (string, error) {
	provider, err := a.base.SelectProvider(seed)
	if err != nil {
		return "", fmt.Errorf("select provider: %w", err)
	}
	return provider, nil
}
func (a *orderStrategyAdapter) SubmitOrder(ctx context.Context, provider string, side schema.TradeSide, quantity string, price *float64) error {
	return submitOrderWithFloat(ctx, a.base, provider, side, quantity, price)
}

type marketMakingAdapter struct {
	base *core.BaseLambda
}

func (a *marketMakingAdapter) Logger() *log.Logger { return a.base.Logger() }
func (a *marketMakingAdapter) GetMarketState() strategies.MarketState {
	state := a.base.GetMarketState()
	return strategies.MarketState{
		LastPrice: state.LastPrice,
		BidPrice:  state.BidPrice,
		AskPrice:  state.AskPrice,
		Spread:    state.Spread,
		SpreadPct: state.SpreadPct,
	}
}
func (a *marketMakingAdapter) GetLastPrice() float64 { return a.base.GetLastPrice() }
func (a *marketMakingAdapter) GetBidPrice() float64  { return a.base.GetBidPrice() }
func (a *marketMakingAdapter) GetAskPrice() float64  { return a.base.GetAskPrice() }
func (a *marketMakingAdapter) IsTradingActive() bool { return a.base.IsTradingActive() }
func (a *marketMakingAdapter) Providers() []string   { return a.base.Providers() }
func (a *marketMakingAdapter) IsDryRun() bool        { return a.base.IsDryRun() }
func (a *marketMakingAdapter) SelectProvider(seed uint64) (string, error) {
	provider, err := a.base.SelectProvider(seed)
	if err != nil {
		return "", fmt.Errorf("select provider: %w", err)
	}
	return provider, nil
}
func (a *marketMakingAdapter) SubmitOrder(ctx context.Context, provider string, side schema.TradeSide, quantity string, price *float64) error {
	return submitOrderWithFloat(ctx, a.base, provider, side, quantity, price)
}
