package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coachpo/meltica/internal/bus/eventbus"
	"github.com/coachpo/meltica/internal/config"
	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/lambda"
	"github.com/coachpo/meltica/internal/lambda/strategies"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/provider"
	"github.com/coachpo/meltica/internal/schema"
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

// StrategyFactory creates trading strategy instances from configuration.
type StrategyFactory func(config map[string]any) (lambda.TradingStrategy, error)

// StrategyConfigField describes a configurable parameter for a strategy.
type StrategyConfigField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Default     any    `json:"default,omitempty"`
	Required    bool   `json:"required"`
}

// StrategyMetadata describes a trading strategy's interface and configuration.
type StrategyMetadata struct {
	Name        string                `json:"name"`
	DisplayName string                `json:"displayName"`
	Description string                `json:"description,omitempty"`
	Config      []StrategyConfigField `json:"config"`
	Events      []schema.RouteType    `json:"events"`
}

// StrategyDefinition combines strategy metadata with a factory function.
type StrategyDefinition struct {
	meta    StrategyMetadata
	factory StrategyFactory
}

// Metadata returns the strategy metadata.
func (d StrategyDefinition) Metadata() StrategyMetadata {
	fields := make([]StrategyConfigField, len(d.meta.Config))
	copy(fields, d.meta.Config)
	events := make([]schema.RouteType, len(d.meta.Events))
	copy(events, d.meta.Events)
	meta := d.meta
	meta.Config = fields
	meta.Events = events
	return meta
}

// ProviderCatalog provides access to available providers.
type ProviderCatalog interface {
	Provider(name string) (provider.Instance, bool)
}

// RouteRegistrar manages dynamic route registration for providers.
type RouteRegistrar interface {
	RegisterLambda(ctx context.Context, lambdaID string, provider string, routes []dispatcher.RouteDeclaration) error
	UnregisterLambda(ctx context.Context, lambdaID string) error
}

// Manager coordinates lambda lifecycle and strategy execution.
type Manager struct {
	mu sync.RWMutex

	bus       eventbus.Bus
	pools     *pool.PoolManager
	providers ProviderCatalog
	logger    *log.Logger
	registrar RouteRegistrar

	strategies map[string]StrategyDefinition
	specs      map[string]config.LambdaSpec
	instances  map[string]*lambdaInstance
}

type lambdaInstance struct {
	base   *lambda.BaseLambda
	cancel context.CancelFunc
	errs   <-chan error
}

// NewManager creates a new lambda manager with the specified dependencies.
func NewManager(bus eventbus.Bus, pools *pool.PoolManager, providers ProviderCatalog, logger *log.Logger, registrar RouteRegistrar) *Manager {
	if logger == nil {
		logger = log.New(os.Stdout, "lambda-manager ", log.LstdFlags|log.Lmicroseconds)
	}
	mgr := &Manager{
		mu:         sync.RWMutex{},
		bus:        bus,
		pools:      pools,
		providers:  providers,
		logger:     logger,
		registrar:  registrar,
		strategies: make(map[string]StrategyDefinition),
		specs:      make(map[string]config.LambdaSpec),
		instances:  make(map[string]*lambdaInstance),
	}
	mgr.registerDefaults()
	return mgr
}

func (m *Manager) registerDefaults() {
	m.registerStrategy(StrategyDefinition{
		meta: StrategyMetadata{
			Name:        "noop",
			DisplayName: "No-Op",
			Description: "Pass-through strategy that performs no actions.",
			Config:      []StrategyConfigField{},
			Events:      []schema.RouteType{},
		},
		factory: func(_ map[string]any) (lambda.TradingStrategy, error) {
			return &strategies.NoOp{}, nil
		},
	})

	m.registerStrategy(StrategyDefinition{
		meta: StrategyMetadata{
			Name:        "delay",
			DisplayName: "Delay",
			Description: "Simulates processing latency with a configurable random delay window.",
			Config: []StrategyConfigField{
				{Name: "min_delay", Type: "duration", Description: "Lower bound for the random delay interval", Default: "100ms", Required: false},
				{Name: "max_delay", Type: "duration", Description: "Upper bound for the random delay interval", Default: "500ms", Required: false},
			},
			Events: []schema.RouteType{},
		},
		factory: func(cfg map[string]any) (lambda.TradingStrategy, error) {
			minDelay := durationValue(cfg, "min_delay", strategies.DefaultMinDelay)
			maxDelay := durationValue(cfg, "max_delay", strategies.DefaultMaxDelay)

			if minDelay < 0 || maxDelay < 0 {
				return nil, fmt.Errorf("delay: min_delay and max_delay must be non-negative")
			}
			if maxDelay < minDelay {
				return nil, fmt.Errorf("delay: max_delay must be greater than or equal to min_delay")
			}

			return &strategies.Delay{
				MinDelay: minDelay,
				MaxDelay: maxDelay,
			}, nil
		},
	})

	m.registerStrategy(StrategyDefinition{
		meta: StrategyMetadata{
			Name:        "logging",
			DisplayName: "Logging",
			Description: "Emits detailed logs for all inbound events.",
			Config: []StrategyConfigField{{
				Name:        "logger_prefix",
				Type:        "string",
				Description: "Prefix prepended to each log message",
				Default:     "[Logging] ",
				Required:    false,
			}},
			Events: []schema.RouteType{},
		},
		factory: func(cfg map[string]any) (lambda.TradingStrategy, error) {
			strat := &strategies.Logging{
				Logger:       log.New(io.Discard, "", 0),
				LoggerPrefix: "",
			}
			strat.LoggerPrefix = stringValue(cfg, "logger_prefix", "[Logging] ")
			return strat, nil
		},
	})

	m.registerStrategy(StrategyDefinition{
		meta: StrategyMetadata{
			Name:        "momentum",
			DisplayName: "Momentum",
			Description: "Trades in the direction of recent price momentum.",
			Config: []StrategyConfigField{
				{Name: "lookback_period", Type: "int", Description: "Number of recent trades used to compute momentum", Default: 20, Required: false},
				{Name: "momentum_threshold", Type: "float", Description: "Minimum momentum (in percent) required to trigger trades", Default: 0.5, Required: false},
				{Name: "order_size", Type: "string", Description: "Quantity for each market order", Default: "1", Required: false},
				{Name: "cooldown", Type: "duration", Description: "Minimum time between trades", Default: "5s", Required: false},
			},
			Events: []schema.RouteType{},
		},
		factory: func(cfg map[string]any) (lambda.TradingStrategy, error) {
			strat := &strategies.Momentum{
				Lambda:            nil,
				LookbackPeriod:    0,
				MomentumThreshold: 0,
				OrderSize:         "",
				Cooldown:          0,
			}
			strat.LookbackPeriod = intValue(cfg, "lookback_period", 20)
			strat.MomentumThreshold = floatValue(cfg, "momentum_threshold", 0.5)
			strat.OrderSize = stringValue(cfg, "order_size", "1")
			strat.Cooldown = durationValue(cfg, "cooldown", 5*time.Second)
			return strat, nil
		},
	})

	m.registerStrategy(StrategyDefinition{
		meta: StrategyMetadata{
			Name:        "meanreversion",
			DisplayName: "Mean Reversion",
			Description: "Trades when price deviates from its moving average.",
			Config: []StrategyConfigField{
				{Name: "window_size", Type: "int", Description: "Moving average window size", Default: 20, Required: false},
				{Name: "deviation_threshold", Type: "float", Description: "Deviation percentage required to open a position", Default: 0.5, Required: false},
				{Name: "order_size", Type: "string", Description: "Order size when entering a position", Default: "1", Required: false},
			},
			Events: []schema.RouteType{},
		},
		factory: func(cfg map[string]any) (lambda.TradingStrategy, error) {
			strat := &strategies.MeanReversion{
				Lambda:             nil,
				WindowSize:         0,
				DeviationThreshold: 0,
				OrderSize:          "",
			}
			strat.WindowSize = intValue(cfg, "window_size", 20)
			strat.DeviationThreshold = floatValue(cfg, "deviation_threshold", 0.5)
			strat.OrderSize = stringValue(cfg, "order_size", "1")
			return strat, nil
		},
	})

	m.registerStrategy(StrategyDefinition{
		meta: StrategyMetadata{
			Name:        "grid",
			DisplayName: "Grid",
			Description: "Places a symmetric buy/sell grid around the reference price.",
			Config: []StrategyConfigField{
				{Name: "grid_levels", Type: "int", Description: "Number of grid levels on each side", Default: 3, Required: false},
				{Name: "grid_spacing", Type: "float", Description: "Grid spacing expressed as percent", Default: 0.5, Required: false},
				{Name: "order_size", Type: "string", Description: "Order size per level", Default: "1", Required: false},
				{Name: "base_price", Type: "float", Description: "Optional base price for the grid", Default: 0.0, Required: false},
			},
			Events: []schema.RouteType{},
		},
		factory: func(cfg map[string]any) (lambda.TradingStrategy, error) {
			strat := &strategies.Grid{
				Lambda:      nil,
				GridLevels:  0,
				GridSpacing: 0,
				OrderSize:   "",
				BasePrice:   0,
			}
			strat.GridLevels = intValue(cfg, "grid_levels", 3)
			strat.GridSpacing = floatValue(cfg, "grid_spacing", 0.5)
			strat.OrderSize = stringValue(cfg, "order_size", "1")
			strat.BasePrice = floatValue(cfg, "base_price", 0)
			return strat, nil
		},
	})

	m.registerStrategy(StrategyDefinition{
		meta: StrategyMetadata{
			Name:        "marketmaking",
			DisplayName: "Market Making",
			Description: "Quotes bid/ask orders around the mid price to capture spread.",
			Config: []StrategyConfigField{
				{Name: "spread_bps", Type: "float", Description: "Spread in basis points", Default: 25.0, Required: false},
				{Name: "order_size", Type: "string", Description: "Quoted order size", Default: "1", Required: false},
				{Name: "max_open_orders", Type: "int", Description: "Maximum concurrent orders per side", Default: 2, Required: false},
			},
			Events: []schema.RouteType{},
		},
		factory: func(cfg map[string]any) (lambda.TradingStrategy, error) {
			strat := &strategies.MarketMaking{
				Lambda:        nil,
				SpreadBps:     0,
				OrderSize:     "",
				MaxOpenOrders: 0,
			}
			strat.SpreadBps = floatValue(cfg, "spread_bps", 25)
			strat.OrderSize = stringValue(cfg, "order_size", "1")
			maxOrders := intValue(cfg, "max_open_orders", 2)
			if maxOrders > int(^uint32(0)>>1) {
				maxOrders = int(^uint32(0) >> 1)
			}
			// #nosec G115 - bounds checked above
			strat.MaxOpenOrders = int32(maxOrders)
			return strat, nil
		},
	})
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
			def.meta.Events = append([]schema.RouteType(nil), strat.SubscribedEvents()...)
		}
	}
	def.meta.Events = append([]schema.RouteType(nil), def.meta.Events...)

	fields := make([]StrategyConfigField, len(def.meta.Config))
	copy(fields, def.meta.Config)
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	def.meta.Config = fields

	m.strategies[name] = def
}

// StrategyCatalog returns all available strategy metadata.
func (m *Manager) StrategyCatalog() []StrategyMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]StrategyMetadata, 0, len(m.strategies))
	for _, def := range m.strategies {
		out = append(out, def.Metadata())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// StrategyDetail returns metadata for a specific strategy by name.
func (m *Manager) StrategyDetail(name string) (StrategyMetadata, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	def, ok := m.strategies[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return StrategyMetadata{
			Name:        "",
			DisplayName: "",
			Description: "",
			Config:      []StrategyConfigField{},
			Events:      []schema.RouteType{},
		}, false
	}
	return def.Metadata(), true
}

// StartFromManifest starts all lambdas defined in the lambda manifest.
func (m *Manager) StartFromManifest(ctx context.Context, manifest config.LambdaManifest) error {
	for _, definition := range manifest.Lambdas {
		spec := sanitizeSpec(definition)
		if err := m.ensureSpec(spec, false); err != nil {
			return err
		}
		if spec.AutoStart {
			if err := m.Start(ctx, spec.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Create creates a new lambda instance from the specification.
func (m *Manager) Create(ctx context.Context, spec config.LambdaSpec) (*lambda.BaseLambda, error) {
	spec = sanitizeSpec(spec)
	if spec.ID == "" || spec.Provider == "" || spec.Symbol == "" || spec.Strategy == "" {
		return nil, fmt.Errorf("strategy instance requires id, provider, symbol, and strategy")
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
	if spec.Config == nil {
		spec.Config = make(map[string]any)
	}
	if _, ok := m.strategies[strings.ToLower(spec.Strategy)]; !ok {
		return fmt.Errorf("strategy %q not registered", spec.Strategy)
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

	_, err = m.launch(ctx, spec)
	return err
}

func (m *Manager) launch(ctx context.Context, spec config.LambdaSpec) (*lambda.BaseLambda, error) {
	providerInst, ok := m.providers.Provider(spec.Provider)
	if !ok {
		return nil, fmt.Errorf("provider %q unavailable", spec.Provider)
	}

	strategy, err := m.buildStrategy(spec.Strategy, spec.Config)
	if err != nil {
		return nil, fmt.Errorf("strategy %s: %w", spec.ID, err)
	}

	var registered bool
	if m.registrar != nil {
		routes := buildRouteDeclarations(strategy, spec)
		if len(routes) > 0 {
			if err := m.registrar.RegisterLambda(ctx, spec.ID, spec.Provider, routes); err != nil {
				return nil, fmt.Errorf("strategy %s: register routes: %w", spec.ID, err)
			}
			registered = true
		}
	}

	base := lambda.NewBaseLambda(spec.ID, lambda.Config{Symbol: spec.Symbol, Provider: spec.Provider}, m.bus, providerInst, m.pools, strategy)
	bindStrategy(strategy, base, m.logger)

	runCtx, cancel := context.WithCancel(ctx)
	errs, err := base.Start(runCtx)
	if err != nil {
		cancel()
		if registered && m.registrar != nil {
			_ = m.registrar.UnregisterLambda(ctx, spec.ID)
		}
		return nil, fmt.Errorf("start strategy %s: %w", spec.ID, err)
	}

	m.mu.Lock()
	m.instances[spec.ID] = &lambdaInstance{base: base, cancel: cancel, errs: errs}
	m.mu.Unlock()

	go m.observe(runCtx, spec.ID, errs)
	return base, nil
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
	if current.Provider != spec.Provider {
		return fmt.Errorf("provider is immutable for %s", spec.ID)
	}
	if current.Symbol != spec.Symbol {
		return fmt.Errorf("symbol is immutable for %s", spec.ID)
	}
	if current.Strategy != spec.Strategy {
		return fmt.Errorf("strategy is immutable for %s", spec.ID)
	}

	spec.AutoStart = current.AutoStart
	if err := m.ensureSpec(spec, true); err != nil {
		return err
	}

	if err := m.Stop(spec.ID); err != nil && !errors.Is(err, ErrInstanceNotRunning) {
		return err
	}
	if _, err := m.launch(ctx, spec); err != nil {
		return err
	}
	return nil
}

// InstanceSnapshot captures the current state of a lambda instance.
type InstanceSnapshot struct {
	ID        string         `json:"id"`
	Strategy  string         `json:"strategy"`
	Provider  string         `json:"provider"`
	Symbol    string         `json:"symbol"`
	Config    map[string]any `json:"config"`
	AutoStart bool           `json:"autoStart"`
	Running   bool           `json:"running"`
}

// Instances returns snapshots of all lambda instances.
func (m *Manager) Instances() []InstanceSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]InstanceSnapshot, 0, len(m.specs))
	for id, spec := range m.specs {
		_, running := m.instances[id]
		out = append(out, snapshotOf(spec, running))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Instance returns a snapshot of a specific lambda instance by ID.
func (m *Manager) Instance(id string) (InstanceSnapshot, bool) {
	spec, err := m.specForID(id)
	if err != nil {
		return InstanceSnapshot{
			ID:        "",
			Strategy:  "",
			Provider:  "",
			Symbol:    "",
			Config:    map[string]any{},
			AutoStart: false,
			Running:   false,
		}, false
	}
	m.mu.RLock()
	_, running := m.instances[spec.ID]
	m.mu.RUnlock()
	return snapshotOf(spec, running), true
}

func snapshotOf(spec config.LambdaSpec, running bool) InstanceSnapshot {
	cfg := copyMap(spec.Config)
	return InstanceSnapshot{
		ID:        spec.ID,
		Strategy:  spec.Strategy,
		Provider:  spec.Provider,
		Symbol:    spec.Symbol,
		Config:    cfg,
		AutoStart: spec.AutoStart,
		Running:   running,
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

func (m *Manager) buildStrategy(name string, cfg map[string]any) (lambda.TradingStrategy, error) {
	def, ok := m.strategies[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil, fmt.Errorf("strategy %q not registered", name)
	}
	return def.factory(copyMap(cfg))
}

func sanitizeSpec(spec config.LambdaSpec) config.LambdaSpec {
	spec.ID = strings.TrimSpace(spec.ID)
	spec.Provider = strings.TrimSpace(spec.Provider)
	spec.Symbol = strings.TrimSpace(spec.Symbol)
	spec.Strategy = strings.TrimSpace(spec.Strategy)
	if spec.Config == nil {
		spec.Config = make(map[string]any)
	}
	return spec
}

func cloneSpec(spec config.LambdaSpec) config.LambdaSpec {
	clone := spec
	clone.Config = copyMap(spec.Config)
	return clone
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

func buildRouteDeclarations(strategy lambda.TradingStrategy, spec config.LambdaSpec) []dispatcher.RouteDeclaration {
	if strategy == nil {
		return nil
	}
	events := strategy.SubscribedEvents()
	if len(events) == 0 {
		return nil
	}
	routes := make([]dispatcher.RouteDeclaration, 0, len(events))
	baseCurrency, quoteCurrency, err := schema.InstrumentCurrencies(spec.Symbol)
	if err != nil {
		baseCurrency, quoteCurrency = "", ""
	}
	seenCurrencies := make(map[string]struct{}, 2)
	for _, typ := range events {
		if err := typ.Validate(); err != nil {
			continue
		}
		switch typ {
		case schema.RouteTypeAccountBalance:
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
				routes = append(routes, dispatcher.RouteDeclaration{
					Type: typ,
					Filters: map[string]any{
						"currency": currency,
					},
				})
			}
		default:
			routes = append(routes, dispatcher.RouteDeclaration{
				Type: typ,
				Filters: map[string]any{
					"instrument": spec.Symbol,
				},
			})
		}
	}
	return routes
}

func bindStrategy(strategy lambda.TradingStrategy, base *lambda.BaseLambda, _ *log.Logger) {
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

func submitOrderWithFloat(ctx context.Context, base *lambda.BaseLambda, side schema.TradeSide, quantity string, price *float64) error {
	var priceStr *string
	if price != nil {
		formatted := strconv.FormatFloat(*price, 'f', -1, 64)
		priceStr = &formatted
	}
	if err := base.SubmitOrder(ctx, side, quantity, priceStr); err != nil {
		return fmt.Errorf("submit order: %w", err)
	}
	return nil
}

type momentumAdapter struct {
	base *lambda.BaseLambda
}

func (a *momentumAdapter) Logger() *log.Logger   { return a.base.Logger() }
func (a *momentumAdapter) GetLastPrice() float64 { return a.base.GetLastPrice() }
func (a *momentumAdapter) IsTradingActive() bool { return a.base.IsTradingActive() }
func (a *momentumAdapter) SubmitMarketOrder(ctx context.Context, side schema.TradeSide, quantity string) error {
	if err := a.base.SubmitMarketOrder(ctx, side, quantity); err != nil {
		return fmt.Errorf("submit market order: %w", err)
	}
	return nil
}

type orderStrategyAdapter struct {
	base *lambda.BaseLambda
}

func (a *orderStrategyAdapter) Logger() *log.Logger   { return a.base.Logger() }
func (a *orderStrategyAdapter) GetLastPrice() float64 { return a.base.GetLastPrice() }
func (a *orderStrategyAdapter) IsTradingActive() bool { return a.base.IsTradingActive() }
func (a *orderStrategyAdapter) SubmitOrder(ctx context.Context, side schema.TradeSide, quantity string, price *float64) error {
	return submitOrderWithFloat(ctx, a.base, side, quantity, price)
}

type marketMakingAdapter struct {
	base *lambda.BaseLambda
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
func (a *marketMakingAdapter) SubmitOrder(ctx context.Context, side schema.TradeSide, quantity string, price *float64) error {
	return submitOrderWithFloat(ctx, a.base, side, quantity, price)
}
