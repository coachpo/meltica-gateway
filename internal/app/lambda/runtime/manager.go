// Package runtime manages lambda lifecycle orchestration and strategy execution.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/coachpo/meltica/internal/app/dispatcher"
	"github.com/coachpo/meltica/internal/app/lambda/core"
	"github.com/coachpo/meltica/internal/app/lambda/js"
	"github.com/coachpo/meltica/internal/app/lambda/strategies"
	"github.com/coachpo/meltica/internal/app/provider"
	"github.com/coachpo/meltica/internal/app/risk"
	"github.com/coachpo/meltica/internal/domain/orderstore"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/domain/strategystore"
	"github.com/coachpo/meltica/internal/infra/bus/eventbus"
	"github.com/coachpo/meltica/internal/infra/config"
	"github.com/coachpo/meltica/internal/infra/pool"
	"github.com/coachpo/meltica/internal/infra/telemetry"
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

const revisionKeySeparator = "\x1f"

type revisionUsage struct {
	strategy  string
	hash      string
	instances map[string]struct{}
	firstSeen time.Time
	lastSeen  time.Time
}

// RevisionUsageSummary captures runtime usage information for a strategy revision.
type RevisionUsageSummary struct {
	Strategy  string    `json:"strategy"`
	Hash      string    `json:"hash"`
	Instances []string  `json:"instances"`
	Count     int       `json:"count"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	IsRunning bool      `json:"running"`
}

// RefreshTargets narrows refresh operations to specific strategies or revision hashes.
type RefreshTargets struct {
	Strategies []string
	Hashes     []string
}

// RefreshResult captures the outcome of a targeted refresh operation.
type RefreshResult struct {
	Selector     string   `json:"selector"`
	Strategy     string   `json:"strategy"`
	Hash         string   `json:"hash"`
	PreviousHash string   `json:"previousHash,omitempty"`
	Instances    []string `json:"instances,omitempty"`
	Reason       string   `json:"reason"`
}

type refreshTargetFilter struct {
	all bool

	selectors   map[string]struct{}
	identifiers map[string]struct{}
	hashes      map[string]struct{}

	requestedSelectors []string
	requestedHashes    []string

	matchedSelectors map[string]bool
	matchedHashes    map[string]bool
}

type refreshMatch struct {
	matched       bool
	byHash        bool
	bySelector    bool
	byIdentifier  bool
	selectorKey   string
	identifierKey string
	hashKey       string
}

func newRevisionUsage(strategy, hash string) *revisionUsage {
	return &revisionUsage{
		strategy:  strategy,
		hash:      hash,
		instances: make(map[string]struct{}, 4),
		firstSeen: time.Time{},
		lastSeen:  time.Time{},
	}
}

func (u *revisionUsage) addInstance(id string, now time.Time) bool {
	if u == nil || id == "" {
		return false
	}
	if u.instances == nil {
		u.instances = make(map[string]struct{}, 4)
	}
	if _, exists := u.instances[id]; exists {
		u.lastSeen = now
		return false
	}
	u.instances[id] = struct{}{}
	if u.firstSeen.IsZero() {
		u.firstSeen = now
	}
	u.lastSeen = now
	return true
}

func (u *revisionUsage) removeInstance(id string, now time.Time) bool {
	if u == nil || id == "" {
		return false
	}
	if _, exists := u.instances[id]; !exists {
		return false
	}
	delete(u.instances, id)
	u.lastSeen = now
	return true
}

func (u *revisionUsage) snapshot() RevisionUsageSummary {
	if u == nil {
		var empty RevisionUsageSummary
		return empty
	}
	out := RevisionUsageSummary{
		Strategy:  u.strategy,
		Hash:      u.hash,
		Instances: nil,
		Count:     len(u.instances),
		FirstSeen: u.firstSeen,
		LastSeen:  u.lastSeen,
		IsRunning: len(u.instances) > 0,
	}
	if len(u.instances) > 0 {
		names := make([]string, 0, len(u.instances))
		for id := range u.instances {
			names = append(names, id)
		}
		sort.Strings(names)
		out.Instances = names
	}
	return out
}

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

	bus              eventbus.Bus
	pools            *pool.PoolManager
	providers        ProviderCatalog
	logger           *log.Logger
	registrar        RouteRegistrar
	riskManager      *risk.Manager
	jsLoader         *js.Loader
	dynamic          map[string]struct{}
	baseline         map[string]struct{}
	dynamicInstances map[string]struct{}
	clock            func() time.Time
	strategyDir      string
	base             map[string]StrategyDefinition

	strategies    map[string]StrategyDefinition
	specs         map[string]config.LambdaSpec
	instances     map[string]*lambdaInstance
	strategyStore strategystore.Store
	orderStore    orderstore.Store

	revisionUsage            map[string]*revisionUsage
	revisionGauge            metric.Int64ObservableGauge
	revisionLifecycleMetric  metric.Int64Counter
	uploadValidationFailures metric.Int64Counter
	tagAssignmentCounter     metric.Int64Counter
	tagDeleteCounter         metric.Int64Counter
}

// Option configures manager behaviour.
type Option func(*Manager)

// WithStrategyStore wires a strategy persistence store into the manager.
func WithStrategyStore(store strategystore.Store) Option {
	return func(m *Manager) {
		m.strategyStore = store
	}
}

// WithOrderStore wires an order persistence store into the manager.
func WithOrderStore(store orderstore.Store) Option {
	return func(m *Manager) {
		m.orderStore = store
	}
}

type lambdaInstance struct {
	base   *core.BaseLambda
	cancel context.CancelFunc
	errs   <-chan error
	strat  core.TradingStrategy
	revKey string
}

// NewManager creates a new lambda manager with the specified dependencies.
func NewManager(cfg config.AppConfig, bus eventbus.Bus, pools *pool.PoolManager, providers ProviderCatalog, logger *log.Logger, registrar RouteRegistrar, opts ...Option) (*Manager, error) {
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
	if cfg.Strategies.RequireRegistry {
		registryPath := filepath.Join(loader.Root(), "registry.json")
		if _, err := os.Stat(registryPath); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("lambda manager: registry required but %s missing", registryPath)
			}
			return nil, fmt.Errorf("lambda manager: check registry: %w", err)
		}
	}

	mgr := &Manager{
		mu:                       sync.RWMutex{},
		lifecycleMu:              sync.RWMutex{},
		lifecycleCtx:             context.Background(),
		bus:                      bus,
		pools:                    pools,
		providers:                providers,
		logger:                   logger,
		registrar:                registrar,
		riskManager:              rm,
		jsLoader:                 loader,
		dynamic:                  make(map[string]struct{}),
		baseline:                 make(map[string]struct{}),
		dynamicInstances:         make(map[string]struct{}),
		clock:                    time.Now,
		strategyDir:              loader.Root(),
		base:                     make(map[string]StrategyDefinition),
		strategies:               make(map[string]StrategyDefinition),
		specs:                    make(map[string]config.LambdaSpec),
		instances:                make(map[string]*lambdaInstance),
		strategyStore:            nil,
		orderStore:               nil,
		revisionUsage:            make(map[string]*revisionUsage),
		revisionGauge:            nil,
		revisionLifecycleMetric:  nil,
		uploadValidationFailures: nil,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(mgr)
		}
	}
	mgr.setupMetrics()
	if _, err := mgr.installJavaScriptStrategies(context.Background()); err != nil {
		return nil, fmt.Errorf("lambda manager: install javascript strategies: %w", err)
	}
	return mgr, nil
}

func (m *Manager) setupMetrics() {
	if m == nil {
		return
	}
	meter := otel.Meter("lambda-manager")
	gauge, err := meter.Int64ObservableGauge("strategy_revision_instances",
		metric.WithDescription("Number of running lambda instances per strategy revision"),
		metric.WithUnit("{instance}"),
		metric.WithInt64Callback(m.observeRevisionUsage),
	)
	if err == nil {
		m.revisionGauge = gauge
	} else if m.logger != nil {
		m.logger.Printf("lambda manager: register revision gauge: %v", err)
	}
	counter, err := meter.Int64Counter("strategy_revision_instances_total",
		metric.WithDescription("Lifecycle transitions for strategy revisions"),
		metric.WithUnit("{event}"),
	)
	if err == nil {
		m.revisionLifecycleMetric = counter
	} else if m.logger != nil {
		m.logger.Printf("lambda manager: register revision counter: %v", err)
	}
	failures, err := meter.Int64Counter("strategy.upload.validation_failure_total",
		metric.WithDescription("Strategy upload validation failures by stage"),
		metric.WithUnit("{event}"),
	)
	if err == nil {
		m.uploadValidationFailures = failures
	} else if m.logger != nil {
		m.logger.Printf("lambda manager: register validation failure counter: %v", err)
	}
	assigned, err := meter.Int64Counter("strategy_tag_reassigned_total",
		metric.WithDescription("Tag reassignments per strategy"),
		metric.WithUnit("{event}"),
	)
	if err == nil {
		m.tagAssignmentCounter = assigned
	} else if m.logger != nil {
		m.logger.Printf("lambda manager: register tag reassignment counter: %v", err)
	}
	deleted, err := meter.Int64Counter("strategy_tag_deleted_total",
		metric.WithDescription("Tag deletion events per strategy"),
		metric.WithUnit("{event}"),
	)
	if err == nil {
		m.tagDeleteCounter = deleted
	} else if m.logger != nil {
		m.logger.Printf("lambda manager: register tag deletion counter: %v", err)
	}
}

func (m *Manager) observeRevisionUsage(_ context.Context, observer metric.Int64Observer) error {
	if m == nil || observer == nil {
		return nil
	}
	m.mu.RLock()
	snapshot := m.revisionUsageSnapshotLocked()
	m.mu.RUnlock()
	env := telemetry.Environment()
	for _, usage := range snapshot {
		observer.Observe(int64(usage.Count), metric.WithAttributes(
			attribute.String("environment", env),
			attribute.String("strategy", usage.Strategy),
			attribute.String("hash", usage.Hash),
		))
	}
	return nil
}

func (m *Manager) recordStrategyValidationFailure(err error) {
	if m == nil {
		return
	}
	diagErr, ok := js.AsDiagnosticError(err)
	if !ok {
		return
	}
	diagnostics := diagErr.Diagnostics()
	if m.logger != nil {
		if len(diagnostics) == 0 {
			m.logger.Printf("strategy validation failed: %v", diagErr)
		} else {
			for _, diag := range diagnostics {
				m.logger.Printf("strategy validation failed: stage=%s message=%s line=%d column=%d hint=%s",
					diag.Stage, diag.Message, diag.Line, diag.Column, diag.Hint)
			}
		}
	}
	if m.uploadValidationFailures == nil {
		return
	}
	env := telemetry.Environment()
	ctx := context.Background()
	if len(diagnostics) == 0 {
		m.uploadValidationFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String("environment", env),
			attribute.String("stage", "unknown"),
		))
		return
	}
	for _, diag := range diagnostics {
		stage := string(diag.Stage)
		if stage == "" {
			stage = "unknown"
		}
		m.uploadValidationFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String("environment", env),
			attribute.String("stage", stage),
		))
	}
}

func (m *Manager) now() time.Time {
	if m == nil || m.clock == nil {
		return time.Now()
	}
	return m.clock()
}

func (m *Manager) ensureRevisionUsageLocked(strategy, hash string) *revisionUsage {
	key := buildRevisionKey(strategy, hash)
	usage, ok := m.revisionUsage[key]
	if !ok {
		usage = newRevisionUsage(strategy, hash)
		m.revisionUsage[key] = usage
	}
	return usage
}

func (m *Manager) markInstanceRunningLocked(spec config.LambdaSpec, instanceID string) string {
	strategy, hash, key := revisionSignatureForSpec(spec)
	usage := m.ensureRevisionUsageLocked(strategy, hash)
	if usage.addInstance(instanceID, m.now()) {
		m.recordRevisionLifecycle(usage, "start")
	}
	return key
}

func (m *Manager) markInstanceStoppedLocked(revisionKey, instanceID string) {
	if revisionKey == "" {
		return
	}
	usage, ok := m.revisionUsage[revisionKey]
	if !ok {
		return
	}
	if usage.removeInstance(instanceID, m.now()) {
		m.recordRevisionLifecycle(usage, "stop")
	}
}

func (m *Manager) revisionUsageSummary(spec config.LambdaSpec) *RevisionUsageSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.revisionUsageSummaryLocked(spec)
}

func (m *Manager) revisionUsageSummaryLocked(spec config.LambdaSpec) *RevisionUsageSummary {
	strategy, hash, key := revisionSignatureForSpec(spec)
	if usage, ok := m.revisionUsage[key]; ok {
		snapshot := usage.snapshot()
		return &snapshot
	}
	summary := RevisionUsageSummary{
		Strategy:  strategy,
		Hash:      hash,
		Instances: nil,
		Count:     0,
		FirstSeen: time.Time{},
		LastSeen:  time.Time{},
		IsRunning: false,
	}
	return &summary
}

func (m *Manager) recordRevisionLifecycle(usage *revisionUsage, action string) {
	if m == nil || usage == nil || m.revisionLifecycleMetric == nil {
		return
	}
	ctx := context.Background()
	m.revisionLifecycleMetric.Add(ctx, 1, metric.WithAttributes(
		attribute.String("environment", telemetry.Environment()),
		attribute.String("strategy", usage.strategy),
		attribute.String("hash", usage.hash),
		attribute.String("action", strings.ToLower(strings.TrimSpace(action))),
	))
}

func (m *Manager) revisionUsageSnapshotLocked() []RevisionUsageSummary {
	if m == nil {
		return nil
	}
	out := make([]RevisionUsageSummary, 0, len(m.revisionUsage))
	for _, usage := range m.revisionUsage {
		out = append(out, usage.snapshot())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Strategy != out[j].Strategy {
			return out[i].Strategy < out[j].Strategy
		}
		return out[i].Hash < out[j].Hash
	})
	return out
}

// RevisionUsageSnapshot returns a stable view of revision usage for external consumers.
func (m *Manager) RevisionUsageSnapshot() []RevisionUsageSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snapshot := m.revisionUsageSnapshotLocked()
	if len(snapshot) == 0 {
		return nil
	}
	out := make([]RevisionUsageSummary, len(snapshot))
	copy(out, snapshot)
	return out
}

func revisionSignatureForSpec(spec config.LambdaSpec) (strategy string, hash string, key string) {
	strategy = normalizeStrategyName(spec.Strategy.Identifier)
	hash = normalizeRevisionHash(spec.Strategy.Hash)
	key = buildRevisionKey(strategy, hash)
	return
}

func normalizeStrategyName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func normalizeRevisionHash(hash string) string {
	return strings.TrimSpace(hash)
}

func buildRevisionKey(strategy, hash string) string {
	return strategy + revisionKeySeparator + hash
}

func normalizeSelector(selector string) string {
	return strings.ToLower(strings.TrimSpace(selector))
}

func newRefreshTargetFilter(targets RefreshTargets) refreshTargetFilter {
	filter := refreshTargetFilter{
		all:                false,
		selectors:          make(map[string]struct{}),
		identifiers:        make(map[string]struct{}),
		hashes:             make(map[string]struct{}),
		requestedSelectors: make([]string, 0, len(targets.Strategies)),
		requestedHashes:    make([]string, 0, len(targets.Hashes)),
		matchedSelectors:   make(map[string]bool),
		matchedHashes:      make(map[string]bool),
	}

	for _, raw := range targets.Strategies {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			selector := normalizeSelector(trimmed)
			if selector != "" {
				filter.selectors[selector] = struct{}{}
				filter.requestedSelectors = append(filter.requestedSelectors, selector)
			}
			name := normalizeStrategyName(trimmed)
			if name != "" {
				filter.identifiers[name] = struct{}{}
			}
		}
	}

	for _, raw := range targets.Hashes {
		if trimmed := normalizeRevisionHash(raw); trimmed != "" {
			filter.hashes[trimmed] = struct{}{}
			filter.requestedHashes = append(filter.requestedHashes, trimmed)
		}
	}

	filter.all = len(filter.selectors) == 0 && len(filter.identifiers) == 0 && len(filter.hashes) == 0
	return filter
}

func (f *refreshTargetFilter) matchSpec(spec config.LambdaSpec) refreshMatch {
	if f == nil {
		var empty refreshMatch
		return empty
	}
	if f.all {
		var match refreshMatch
		match.matched = true
		return match
	}

	var match refreshMatch

	hash := normalizeRevisionHash(spec.Strategy.Hash)
	if hash != "" {
		if _, ok := f.hashes[hash]; ok {
			match.matched = true
			match.byHash = true
			match.hashKey = hash
		}
	}

	selector := normalizeSelector(spec.Strategy.Selector)
	if selector != "" {
		if _, ok := f.selectors[selector]; ok {
			match.matched = true
			match.bySelector = true
			match.selectorKey = selector
		}
	}

	identifier := normalizeStrategyName(spec.Strategy.Identifier)
	if identifier != "" {
		if _, ok := f.identifiers[identifier]; ok {
			match.matched = true
			match.byIdentifier = true
			match.identifierKey = identifier
		}
	}

	if match.bySelector && match.selectorKey == "" {
		match.selectorKey = selector
	}
	if match.byIdentifier && match.identifierKey == "" {
		match.identifierKey = identifier
	}

	return match
}

func (f *refreshTargetFilter) recordMatch(match refreshMatch) {
	if f == nil || f.all || !match.matched {
		return
	}
	if match.byHash && match.hashKey != "" {
		if f.matchedHashes == nil {
			f.matchedHashes = make(map[string]bool, len(f.hashes))
		}
		f.matchedHashes[match.hashKey] = true
	}
	if match.bySelector && match.selectorKey != "" {
		if f.matchedSelectors == nil {
			f.matchedSelectors = make(map[string]bool, len(f.selectors)+len(f.identifiers))
		}
		f.matchedSelectors[match.selectorKey] = true
	}
	if match.byIdentifier && match.identifierKey != "" {
		if f.matchedSelectors == nil {
			f.matchedSelectors = make(map[string]bool, len(f.selectors)+len(f.identifiers))
		}
		f.matchedSelectors[match.identifierKey] = true
	}
}

func (f refreshTargetFilter) unmatchedHashTargets() []string {
	if f.all || len(f.requestedHashes) == 0 {
		return nil
	}
	var out []string
	for _, hash := range f.requestedHashes {
		if f.matchedHashes != nil && f.matchedHashes[hash] {
			continue
		}
		out = append(out, hash)
	}
	return out
}

func (f refreshTargetFilter) unmatchedSelectors() []string {
	if f.all || len(f.requestedSelectors) == 0 {
		return nil
	}
	var out []string
	for _, selector := range f.requestedSelectors {
		if f.matchedSelectors != nil && f.matchedSelectors[selector] {
			continue
		}
		out = append(out, selector)
	}
	return out
}

func ensureRefreshResult(results map[string]*RefreshResult, id string, spec config.LambdaSpec) *RefreshResult {
	if results == nil {
		return nil
	}
	if existing, ok := results[id]; ok {
		return existing
	}
	selector := spec.Strategy.Selector
	if selector == "" {
		selector = spec.Strategy.Identifier
	}
	result := &RefreshResult{
		Selector:     selector,
		Strategy:     spec.Strategy.Identifier,
		Hash:         spec.Strategy.Hash,
		PreviousHash: spec.Strategy.Hash,
		Instances:    []string{id},
		Reason:       "",
	}
	results[id] = result
	return result
}

func pickRefreshReason(current, candidate string) string {
	switch candidate {
	case "":
		return current
	case "retired":
		return "retired"
	case "refreshed":
		if current == "" || current == "alreadyPinned" {
			return "refreshed"
		}
		return current
	case "alreadyPinned":
		if current == "" {
			return "alreadyPinned"
		}
		return current
	default:
		if current == "" {
			return candidate
		}
		return current
	}
}

func buildUnmatchedRefreshResults(filter refreshTargetFilter) []RefreshResult {
	if filter.all {
		return nil
	}
	var out []RefreshResult
	for _, hash := range filter.unmatchedHashTargets() {
		out = append(out, RefreshResult{
			Selector:     hash,
			Strategy:     "",
			Hash:         hash,
			PreviousHash: hash,
			Instances:    nil,
			Reason:       "retired",
		})
	}
	for _, selector := range filter.unmatchedSelectors() {
		out = append(out, RefreshResult{
			Selector:     selector,
			Strategy:     "",
			Hash:         "",
			PreviousHash: "",
			Instances:    nil,
			Reason:       "retired",
		})
	}
	return out
}

func cloneRevisionUsage(src *RevisionUsageSummary) *RevisionUsageSummary {
	if src == nil {
		return nil
	}
	cloned := *src
	if len(src.Instances) > 0 {
		cloned.Instances = append([]string(nil), src.Instances...)
	}
	return &cloned
}

func convertModuleUsageSnapshots(usages []RevisionUsageSummary) []js.ModuleUsageSnapshot {
	if len(usages) == 0 {
		return nil
	}
	out := make([]js.ModuleUsageSnapshot, 0, len(usages))
	for _, usage := range usages {
		snapshot := js.ModuleUsageSnapshot{
			Name:      usage.Strategy,
			Hash:      usage.Hash,
			Instances: append([]string(nil), usage.Instances...),
			Count:     usage.Count,
			FirstSeen: usage.FirstSeen,
			LastSeen:  usage.LastSeen,
		}
		out = append(out, snapshot)
	}
	return out
}

// RevisionUsageFor returns usage metadata for the specified strategy revision.
func (m *Manager) RevisionUsageFor(strategy, hash string) RevisionUsageSummary {
	spec := config.LambdaSpec{
		ID:              "",
		Strategy:        config.LambdaStrategySpec{Identifier: strategy, Config: nil, Selector: "", Tag: "", Hash: hash},
		ProviderSymbols: nil,
		Providers:       nil,
	}
	summary := m.revisionUsageSummary(spec)
	if summary == nil {
		return RevisionUsageSummary{
			Strategy:  normalizeStrategyName(strategy),
			Hash:      normalizeRevisionHash(hash),
			Instances: nil,
			Count:     0,
			FirstSeen: time.Time{},
			LastSeen:  time.Time{},
			IsRunning: false,
		}
	}
	cloned := cloneRevisionUsage(summary)
	if cloned == nil {
		var empty RevisionUsageSummary
		return empty
	}
	return *cloned
}

// RevisionInstances returns instance summaries pinned to the specified revision.
func (m *Manager) RevisionInstances(strategy, hash string, includeStopped bool) []InstanceSummary {
	normalizedStrategy := normalizeStrategyName(strategy)
	normalizedHash := normalizeRevisionHash(hash)

	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]InstanceSummary, 0)
	for id, spec := range m.specs {
		specStrategy := normalizeStrategyName(spec.Strategy.Identifier)
		specHash := normalizeRevisionHash(spec.Strategy.Hash)
		if specStrategy != normalizedStrategy || specHash != normalizedHash {
			continue
		}
		_, running := m.instances[id]
		if !running && !includeStopped {
			continue
		}
		usage := m.revisionUsageSummaryLocked(spec)
		out = append(out, summaryOf(spec, running, usage))
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
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
	var targets RefreshTargets
	_, err := m.refreshJavaScriptStrategies(ctx, targets)
	return err
}

// RefreshJavaScriptStrategiesWithTargets performs a filtered refresh limited to the supplied targets.
func (m *Manager) RefreshJavaScriptStrategiesWithTargets(ctx context.Context, targets RefreshTargets) ([]RefreshResult, error) {
	return m.refreshJavaScriptStrategies(ctx, targets)
}

func (m *Manager) refreshJavaScriptStrategies(ctx context.Context, targets RefreshTargets) ([]RefreshResult, error) {
	if _, err := m.installJavaScriptStrategies(ctx); err != nil {
		return nil, err
	}
	selections := m.snapshotStrategySelections()
	filter := newRefreshTargetFilter(targets)
	if len(selections) == 0 {
		results := buildUnmatchedRefreshResults(filter)
		return results, nil
	}

	dynamicSet := m.currentDynamicSet()
	updates := make(map[string]config.LambdaSpec)
	restartIDs := make([]string, 0)
	stopOnly := make([]string, 0)
	resultsByInstance := make(map[string]*RefreshResult)

	for id, selection := range selections {
		spec := selection.Spec
		name := strings.ToLower(strings.TrimSpace(spec.Strategy.Identifier))
		if _, ok := dynamicSet[name]; !ok && spec.Strategy.Hash == "" {
			continue
		}
		match := filter.matchSpec(spec)
		if !match.matched {
			continue
		}
		filter.recordMatch(match)

		result := ensureRefreshResult(resultsByInstance, id, spec)
		selector := spec.Strategy.Selector
		if selector == "" {
			selector = spec.Strategy.Identifier
		}
		if selector == "" || m.jsLoader == nil {
			result.Reason = pickRefreshReason(result.Reason, "retired")
			stopOnly = append(stopOnly, id)
			continue
		}
		resolution, err := m.jsLoader.ResolveReference(selector)
		if err != nil {
			result.Reason = pickRefreshReason(result.Reason, "retired")
			stopOnly = append(stopOnly, id)
			continue
		}

		oldHash := spec.Strategy.Hash
		spec.Strategy.Identifier = resolution.Name
		spec.Strategy.Hash = resolution.Hash
		spec.Strategy.Tag = resolution.Tag
		spec.Strategy.Selector = canonicalSelector(selector, resolution)
		updates[id] = spec

		result.Hash = resolution.Hash
		result.Selector = spec.Strategy.Selector
		result.Strategy = spec.Strategy.Identifier
		result.Reason = pickRefreshReason(result.Reason, "alreadyPinned")
		if oldHash != resolution.Hash {
			result.Reason = pickRefreshReason(result.Reason, "refreshed")
		}

		if selection.Running && oldHash != resolution.Hash {
			restartIDs = append(restartIDs, id)
		}
	}

	if len(updates) > 0 {
		m.mu.Lock()
		for id, updated := range updates {
			strategy, hash, _ := revisionSignatureForSpec(updated)
			m.ensureRevisionUsageLocked(strategy, hash)
			m.specs[id] = cloneSpec(updated)
		}
		m.mu.Unlock()
	}

	for _, id := range restartIDs {
		if err := m.Stop(id); err != nil && !errors.Is(err, ErrInstanceNotRunning) {
			if m.logger != nil {
				m.logger.Printf("stop strategy %s: %v", id, err)
			}
		}
	}
	for _, id := range restartIDs {
		if err := m.Start(ctx, id); err != nil && m.logger != nil {
			if !errors.Is(err, ErrInstanceAlreadyRunning) {
				m.logger.Printf("restart strategy %s: %v", id, err)
			}
		}
	}
	for _, id := range stopOnly {
		if err := m.Stop(id); err != nil && !errors.Is(err, ErrInstanceNotRunning) {
			if m.logger != nil {
				m.logger.Printf("stop strategy %s: %v", id, err)
			}
		}
	}

	results := make([]RefreshResult, 0, len(resultsByInstance))
	for _, res := range resultsByInstance {
		if res.Reason == "" {
			res.Reason = "alreadyPinned"
		}
		results = append(results, *res)
	}

	unmatched := buildUnmatchedRefreshResults(filter)
	results = append(results, unmatched...)

	sort.Slice(results, func(i, j int) bool {
		if results[i].Selector != results[j].Selector {
			return results[i].Selector < results[j].Selector
		}
		return results[i].Strategy < results[j].Strategy
	})

	return results, nil
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

// Create creates a new lambda instance from the specification.
func (m *Manager) Create(spec config.LambdaSpec) (*core.BaseLambda, error) {
	spec = sanitizeSpec(spec)
	if spec.ID == "" || len(spec.Providers) == 0 || spec.Strategy.Identifier == "" {
		return nil, fmt.Errorf("strategy instance requires id, providers, and strategy")
	}
	if len(spec.AllSymbols()) == 0 {
		return nil, fmt.Errorf("strategy %s: instrument symbols required", spec.ID)
	}
	if err := m.ensureSpec(&spec, false); err != nil {
		return nil, fmt.Errorf("ensure spec %s: %w", spec.ID, err)
	}
	m.setBaselineInstance(spec.ID, false)
	m.setDynamicInstance(spec.ID, true)
	m.persistStrategy(spec.ID)
	return nil, nil
}

func (m *Manager) ensureSpec(spec *config.LambdaSpec, allowReplace bool) error {
	if spec == nil {
		return fmt.Errorf("lambda spec required")
	}
	if spec.Strategy.Config == nil {
		spec.Strategy.Config = make(map[string]any)
	}

	rawIdentifier := strings.TrimSpace(spec.Strategy.Identifier)
	baseName := strings.ToLower(rawIdentifier)
	requireResolution := strings.ContainsAny(rawIdentifier, ":@")
	if !requireResolution {
		if current := m.currentDynamicSet(); len(current) > 0 {
			if _, ok := current[baseName]; ok {
				requireResolution = true
			}
		}
	}

	if requireResolution {
		if m.jsLoader == nil {
			return fmt.Errorf("strategy loader unavailable")
		}
		res, err := m.jsLoader.ResolveReference(rawIdentifier)
		if err != nil {
			return fmt.Errorf("resolve strategy %q: %w", rawIdentifier, err)
		}
		spec.Strategy.Identifier = res.Name
		spec.Strategy.Hash = res.Hash
		spec.Strategy.Tag = res.Tag
		spec.Strategy.Selector = canonicalSelector(rawIdentifier, res)
	} else {
		spec.Strategy.Identifier = strings.ToLower(rawIdentifier)
		spec.Strategy.Selector = spec.Strategy.Identifier
		spec.Strategy.Hash = ""
		spec.Strategy.Tag = ""
	}

	name := strings.ToLower(strings.TrimSpace(spec.Strategy.Identifier))
	if _, ok := m.strategies[name]; !ok {
		return fmt.Errorf("strategy %q not registered", spec.Strategy.Identifier)
	}

	m.mu.Lock()
	if _, exists := m.specs[spec.ID]; exists && !allowReplace {
		m.mu.Unlock()
		return ErrInstanceExists
	}
	strategy, hash, _ := revisionSignatureForSpec(*spec)
	m.ensureRevisionUsageLocked(strategy, hash)
	m.specs[spec.ID] = cloneSpec(*spec)
	m.mu.Unlock()

	m.persistStrategy(spec.ID)
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

	strategy, err := m.buildStrategy(spec.Strategy)
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
	base := core.NewBaseLambda(spec.ID, baseCfg, m.bus, orderRouter, m.pools, strategy, m.riskManager, m.orderStore)
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
	revisionKey := m.markInstanceRunningLocked(spec, spec.ID)
	m.instances[spec.ID] = &lambdaInstance{base: base, cancel: cancel, errs: errs, strat: strategy, revKey: revisionKey}
	m.mu.Unlock()

	go m.observe(runCtx, spec.ID, errs, strategy)
	m.persistStrategy(spec.ID)
	return base, resolvedProviders, routes, nil
}

func (m *Manager) specForID(id string) (config.LambdaSpec, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		var empty config.LambdaSpec
		return empty, ErrInstanceNotFound
	}
	m.mu.RLock()
	spec, ok := m.specs[id]
	m.mu.RUnlock()
	if !ok {
		var empty config.LambdaSpec
		return empty, ErrInstanceNotFound
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
	revKey := inst.revKey
	delete(m.instances, id)
	m.markInstanceStoppedLocked(revKey, id)
	m.mu.Unlock()

	inst.cancel()
	if m.registrar != nil {
		_ = m.registrar.UnregisterLambda(context.Background(), id)
	}
	closeStrategy(inst.strat)
	m.persistStrategy(id)
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
	delete(m.baseline, strings.ToLower(strings.TrimSpace(id)))
	delete(m.dynamicInstances, strings.ToLower(strings.TrimSpace(id)))
	m.deleteStrategy(id)
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
	if err := m.ensureSpec(&spec, true); err != nil {
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
	ID                 string                `json:"id"`
	StrategyIdentifier string                `json:"strategyIdentifier"`
	StrategyTag        string                `json:"strategyTag,omitempty"`
	StrategyHash       string                `json:"strategyHash,omitempty"`
	StrategySelector   string                `json:"strategySelector,omitempty"`
	Providers          []string              `json:"providers"`
	AggregatedSymbols  []string              `json:"aggregatedSymbols"`
	Running            bool                  `json:"running"`
	Usage              *RevisionUsageSummary `json:"usage,omitempty"`
}

// InstanceSnapshot captures the detailed state of a lambda instance.
type InstanceSnapshot struct {
	ID                string                            `json:"id"`
	Strategy          config.LambdaStrategySpec         `json:"strategy"`
	Providers         []string                          `json:"providers"`
	ProviderSymbols   map[string]config.ProviderSymbols `json:"scope"`
	AggregatedSymbols []string                          `json:"aggregatedSymbols"`
	Running           bool                              `json:"running"`
	Usage             *RevisionUsageSummary             `json:"usage,omitempty"`
}

// Instances returns summaries of all lambda instances.
func (m *Manager) Instances() []InstanceSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]InstanceSummary, 0, len(m.specs))
	for id, spec := range m.specs {
		_, running := m.instances[id]
		usage := m.revisionUsageSummaryLocked(spec)
		out = append(out, summaryOf(spec, running, usage))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Instance returns a snapshot of a specific lambda instance by ID.
func (m *Manager) Instance(id string) (InstanceSnapshot, bool) {
	spec, err := m.specForID(id)
	if err != nil {
		return InstanceSnapshot{
			ID: "",
			Strategy: config.LambdaStrategySpec{
				Identifier: "",
				Selector:   "",
				Tag:        "",
				Hash:       "",
				Config:     map[string]any{},
			},
			Providers:         []string{},
			ProviderSymbols:   map[string]config.ProviderSymbols{},
			AggregatedSymbols: []string{},
			Running:           false,
			Usage:             nil,
		}, false
	}
	m.mu.RLock()
	_, running := m.instances[spec.ID]
	usage := m.revisionUsageSummaryLocked(spec)
	m.mu.RUnlock()
	return snapshotOf(spec, running, usage), true
}

// IsBaseline reports whether the instance originated from the baseline manifest.
func (m *Manager) IsBaseline(id string) bool {
	return m.isBaselineInstance(id)
}

// IsDynamic reports whether the instance was created dynamically via control APIs.
func (m *Manager) IsDynamic(id string) bool {
	return m.isDynamicInstance(id)
}

func summaryOf(spec config.LambdaSpec, running bool, usage *RevisionUsageSummary) InstanceSummary {
	providers := append([]string(nil), spec.Providers...)
	aggregated := spec.AllSymbols()
	return InstanceSummary{
		ID:                 spec.ID,
		StrategyIdentifier: spec.Strategy.Identifier,
		StrategyTag:        spec.Strategy.Tag,
		StrategyHash:       spec.Strategy.Hash,
		StrategySelector:   spec.Strategy.Selector,
		Providers:          providers,
		AggregatedSymbols:  aggregated,
		Running:            running,
		Usage:              cloneRevisionUsage(usage),
	}
}

func snapshotOf(spec config.LambdaSpec, running bool, usage *RevisionUsageSummary) InstanceSnapshot {
	strategyConfig := copyMap(spec.Strategy.Config)
	providers := append([]string(nil), spec.Providers...)
	assignments := cloneProviderSymbols(spec.ProviderSymbols)
	aggregated := spec.AllSymbols()
	return InstanceSnapshot{
		ID: spec.ID,
		Strategy: config.LambdaStrategySpec{
			Identifier: spec.Strategy.Identifier,
			Config:     strategyConfig,
			Selector:   spec.Strategy.Selector,
			Tag:        spec.Strategy.Tag,
			Hash:       spec.Strategy.Hash,
		},
		Providers:         providers,
		ProviderSymbols:   assignments,
		AggregatedSymbols: aggregated,
		Running:           running,
		Usage:             cloneRevisionUsage(usage),
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

func (m *Manager) buildStrategy(spec config.LambdaStrategySpec) (core.TradingStrategy, error) {
	name := strings.ToLower(strings.TrimSpace(spec.Identifier))
	if name == "" {
		return nil, fmt.Errorf("strategy identifier required")
	}
	if spec.Hash != "" && m.jsLoader != nil {
		module, err := m.jsLoader.Get(spec.Hash)
		if err != nil {
			if errors.Is(err, js.ErrModuleNotFound) {
				return nil, fmt.Errorf("strategy %s: revision %s unavailable", name, spec.Hash)
			}
			return nil, fmt.Errorf("strategy %s: %w", name, err)
		}
		if module == nil {
			return nil, fmt.Errorf("strategy %s: revision %s unavailable", name, spec.Hash)
		}
		if !strings.EqualFold(module.Name, name) {
			return nil, fmt.Errorf("strategy %s: revision %s belongs to %s", name, spec.Hash, module.Name)
		}
		strategy, buildErr := js.NewStrategy(module, spec.Config, m.logger)
		if buildErr != nil {
			return nil, fmt.Errorf("strategy %s: %w", name, buildErr)
		}
		return strategy, nil
	}
	def, ok := m.strategies[name]
	if !ok {
		return nil, fmt.Errorf("strategy %q not registered", spec.Identifier)
	}
	return def.factory(copyMap(spec.Config))
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
	clone.Strategy.Selector = spec.Strategy.Selector
	clone.Strategy.Tag = spec.Strategy.Tag
	clone.Strategy.Hash = spec.Strategy.Hash
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

func cloneSymbolMap(src map[string]config.ProviderSymbols) map[string][]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]string, len(src))
	for provider, assignment := range src {
		out[provider] = append([]string(nil), assignment.Symbols...)
	}
	return out
}

func buildProviderSymbols(symbols map[string][]string) map[string]config.ProviderSymbols {
	if len(symbols) == 0 {
		return make(map[string]config.ProviderSymbols)
	}
	out := make(map[string]config.ProviderSymbols, len(symbols))
	for provider, vals := range symbols {
		out[provider] = config.ProviderSymbols{Symbols: append([]string(nil), vals...)}
	}
	return out
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

func (m *Manager) strategySnapshot(id string) (strategystore.Snapshot, bool) {
	m.mu.RLock()
	spec, ok := m.specs[id]
	if !ok {
		m.mu.RUnlock()
		var empty strategystore.Snapshot
		return empty, false
	}
	_, running := m.instances[id]
	m.mu.RUnlock()

	snapshot := strategystore.Snapshot{
		ID:              spec.ID,
		Strategy:        strategystore.Strategy{Identifier: spec.Strategy.Identifier, Selector: spec.Strategy.Selector, Tag: spec.Strategy.Tag, Hash: spec.Strategy.Hash, Config: copyMap(spec.Strategy.Config)},
		Providers:       append([]string(nil), spec.Providers...),
		ProviderSymbols: cloneSymbolMap(spec.ProviderSymbols),
		Running:         running,
		Dynamic:         m.isDynamicInstance(spec.ID),
		Baseline:        m.isBaselineInstance(spec.ID),
		Metadata:        map[string]any{},
		UpdatedAt:       m.clock(),
	}
	return snapshot, true
}

func (m *Manager) persistStrategy(id string) {
	if m == nil || m.strategyStore == nil {
		return
	}
	snapshot, ok := m.strategySnapshot(id)
	if !ok {
		return
	}
	ctx := m.parentContext()
	if err := m.strategyStore.Save(ctx, snapshot); err != nil && m.logger != nil {
		m.logger.Printf("strategy/%s: persist failed: %v", id, err)
	}
}

func (m *Manager) deleteStrategy(id string) {
	if m == nil || m.strategyStore == nil {
		return
	}
	ctx := m.parentContext()
	if err := m.strategyStore.Delete(ctx, id); err != nil && m.logger != nil {
		m.logger.Printf("strategy/%s: delete snapshot failed: %v", id, err)
	}
}

func (m *Manager) restoreStrategySnapshot(ctx context.Context, snapshot strategystore.Snapshot) {
	if snapshot.ID == "" {
		return
	}
	spec := specFromSnapshot(snapshot)
	if err := m.ensureSpec(&spec, true); err != nil {
		if m.logger != nil {
			m.logger.Printf("strategy/%s: restore spec failed: %v", snapshot.ID, err)
		}
		return
	}
	m.setBaselineInstance(snapshot.ID, snapshot.Baseline)
	m.setDynamicInstance(snapshot.ID, snapshot.Dynamic)
	if snapshot.Running {
		if err := m.Start(ctx, snapshot.ID); err != nil && m.logger != nil {
			if !errors.Is(err, ErrInstanceAlreadyRunning) {
				m.logger.Printf("strategy/%s: restore start failed: %v", snapshot.ID, err)
			}
		}
	}
}

// RestoreSnapshot rehydrates a strategy instance snapshot without failing the manager on errors.
func (m *Manager) RestoreSnapshot(ctx context.Context, snapshot strategystore.Snapshot) {
	if m == nil {
		return
	}
	m.restoreStrategySnapshot(ctx, snapshot)
}

func specFromSnapshot(snapshot strategystore.Snapshot) config.LambdaSpec {
	spec := config.LambdaSpec{
		ID:              snapshot.ID,
		Strategy:        config.LambdaStrategySpec{Identifier: snapshot.Strategy.Identifier, Config: copyMap(snapshot.Strategy.Config), Selector: snapshot.Strategy.Selector, Tag: snapshot.Strategy.Tag, Hash: snapshot.Strategy.Hash},
		Providers:       append([]string(nil), snapshot.Providers...),
		ProviderSymbols: buildProviderSymbols(snapshot.ProviderSymbols),
	}
	if len(snapshot.Providers) > 0 && len(spec.ProviderSymbols) == 0 {
		spec.Providers = append([]string(nil), snapshot.Providers...)
	} else {
		spec.RefreshProviders()
	}
	return sanitizeSpec(spec)
}

func (m *Manager) setBaselineInstance(id string, baseline bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if baseline {
		m.baseline[strings.ToLower(strings.TrimSpace(id))] = struct{}{}
	} else {
		delete(m.baseline, strings.ToLower(strings.TrimSpace(id)))
	}
}

func (m *Manager) isBaselineInstance(id string) bool {
	if m == nil || id == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.baseline[strings.ToLower(strings.TrimSpace(id))]
	return ok
}

func (m *Manager) setDynamicInstance(id string, dynamic bool) {
	if m == nil || id == "" {
		return
	}
	key := strings.ToLower(strings.TrimSpace(id))
	m.mu.Lock()
	defer m.mu.Unlock()
	if dynamic {
		m.dynamicInstances[key] = struct{}{}
	} else {
		delete(m.dynamicInstances, key)
	}
}

func (m *Manager) isDynamicInstance(id string) bool {
	if m == nil || id == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.dynamicInstances[strings.ToLower(strings.TrimSpace(id))]
	return ok
}

type strategySelection struct {
	Spec    config.LambdaSpec
	Running bool
}

func (m *Manager) snapshotStrategySelections() map[string]strategySelection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]strategySelection, len(m.specs))
	for id, spec := range m.specs {
		_, running := m.instances[id]
		out[id] = strategySelection{
			Spec:    cloneSpec(spec),
			Running: running,
		}
	}
	return out
}

func canonicalSelector(raw string, res js.ModuleResolution) string {
	name := strings.ToLower(strings.TrimSpace(res.Name))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(raw))
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return name
	}
	if strings.Contains(trimmed, "@") && res.Hash != "" {
		return fmt.Sprintf("%s@%s", name, res.Hash)
	}
	if strings.Contains(trimmed, ":") && res.Tag != "" {
		return fmt.Sprintf("%s:%s", name, res.Tag)
	}
	return name
}

// StrategyModules returns metadata for the currently loaded JavaScript strategy modules.
func (m *Manager) StrategyModules() []js.ModuleSummary {
	if m == nil || m.jsLoader == nil {
		return nil
	}
	usage := convertModuleUsageSnapshots(m.RevisionUsageSnapshot())
	return m.jsLoader.ListWithUsage(usage)
}

// StrategyModule returns module metadata for a specific strategy.
func (m *Manager) StrategyModule(name string) (js.ModuleSummary, error) {
	if m == nil || m.jsLoader == nil {
		return js.ModuleSummary{}, js.ErrModuleNotFound
	}
	usage := convertModuleUsageSnapshots(m.RevisionUsageSnapshot())
	summary, err := m.jsLoader.ModuleWithUsage(name, usage)
	if err != nil {
		return js.ModuleSummary{}, fmt.Errorf("strategy module %q: %w", name, err)
	}
	return summary, nil
}

// ResolveStrategySelector resolves a module selector into the corresponding revision.
func (m *Manager) ResolveStrategySelector(selector string) (js.ModuleResolution, error) {
	if m == nil || m.jsLoader == nil {
		var empty js.ModuleResolution
		return empty, fmt.Errorf("strategy loader unavailable")
	}
	resolution, err := m.jsLoader.ResolveReference(selector)
	if err != nil {
		var empty js.ModuleResolution
		return empty, fmt.Errorf("resolve selector %q: %w", selector, err)
	}
	return resolution, nil
}

// RevisionUsageDetail resolves a selector and returns usage metadata with matching instances.
func (m *Manager) RevisionUsageDetail(selector string, includeStopped bool) (RevisionUsageSummary, string, []InstanceSummary, error) {
	resolution, err := m.ResolveStrategySelector(selector)
	if err != nil {
		var empty RevisionUsageSummary
		return empty, "", nil, err
	}
	summary := m.RevisionUsageFor(resolution.Name, resolution.Hash)
	canonical := canonicalSelector(selector, resolution)
	if canonical == "" {
		canonical = selector
	}
	instances := m.RevisionInstances(resolution.Name, resolution.Hash, includeStopped)
	return summary, canonical, instances, nil
}

// RegistryExport returns the registry manifest alongside usage summaries.
func (m *Manager) RegistryExport() (js.RegistrySnapshot, []RevisionUsageSummary, error) {
	if m == nil || m.jsLoader == nil {
		return nil, nil, fmt.Errorf("strategy loader unavailable")
	}
	snapshot, err := m.jsLoader.RegistrySnapshot()
	if err != nil {
		return nil, nil, fmt.Errorf("registry snapshot: %w", err)
	}
	usage := m.RevisionUsageSnapshot()
	return snapshot, usage, nil
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

// UpsertStrategy writes or replaces a JavaScript strategy module.
func (m *Manager) UpsertStrategy(source []byte, opts js.ModuleWriteOptions) (js.ModuleResolution, error) {
	if m == nil || m.jsLoader == nil {
		return js.ModuleResolution{Name: "", Hash: "", Tag: "", Alias: "", Module: nil}, fmt.Errorf("strategy loader unavailable")
	}
	resolution, err := m.jsLoader.Store(source, opts)
	if err == nil {
		return resolution, nil
	}
	m.recordStrategyValidationFailure(err)
	if !errors.Is(err, js.ErrRegistryUnavailable) {
		return js.ModuleResolution{Name: "", Hash: "", Tag: "", Alias: "", Module: nil}, fmt.Errorf("strategy upsert: %w", err)
	}
	filename := opts.Filename
	if strings.TrimSpace(filename) == "" {
		filename = "strategy.js"
	}
	if err := m.jsLoader.Write(filename, source); err != nil {
		m.recordStrategyValidationFailure(err)
		return js.ModuleResolution{Name: "", Hash: "", Tag: "", Alias: "", Module: nil}, fmt.Errorf("strategy upsert %q: %w", filename, err)
	}
	return js.ModuleResolution{Name: "", Hash: "", Tag: "", Alias: "", Module: nil}, nil
}

// AssignStrategyTag re-points the supplied tag alias to the provided revision hash.
func (m *Manager) AssignStrategyTag(ctx context.Context, name, tag, hash string, refresh bool) (string, error) {
	if m == nil || m.jsLoader == nil {
		return "", fmt.Errorf("strategy loader unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	previous, err := m.jsLoader.AssignTag(name, tag, hash)
	if err != nil {
		return "", fmt.Errorf("assign tag %s:%s: %w", name, tag, err)
	}
	if m.logger != nil {
		m.logger.Printf("strategy tag %s:%s moved from %s to %s", name, tag, previous, hash)
	}
	if refresh && !strings.EqualFold(previous, hash) {
		_, refreshErr := m.RefreshJavaScriptStrategiesWithTargets(ctx, RefreshTargets{Strategies: []string{name}})
		if refreshErr != nil {
			return previous, fmt.Errorf("refresh after tag move: %w", refreshErr)
		}
	}
	if m.tagAssignmentCounter != nil && !strings.EqualFold(previous, hash) {
		env := telemetry.Environment()
		m.tagAssignmentCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("environment", env),
			attribute.String("strategy", strings.ToLower(strings.TrimSpace(name))),
			attribute.String("tag", strings.ToLower(strings.TrimSpace(tag))),
		))
	}
	return previous, nil
}

// DeleteStrategyTag removes a tag alias while honoring guard rails.
func (m *Manager) DeleteStrategyTag(name, tag string, allowOrphan bool) (string, error) {
	if m == nil || m.jsLoader == nil {
		return "", fmt.Errorf("strategy loader unavailable")
	}
	opts := js.TagDeleteOptions{AllowOrphan: allowOrphan}
	hash, err := m.jsLoader.DeleteTagWithOptions(name, tag, opts)
	if err != nil {
		return "", fmt.Errorf("delete tag %s:%s: %w", name, tag, err)
	}
	if m.logger != nil {
		m.logger.Printf("strategy tag %s:%s removed (hash %s)", name, tag, hash)
	}
	if m.tagDeleteCounter != nil {
		env := telemetry.Environment()
		m.tagDeleteCounter.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("environment", env),
			attribute.String("strategy", strings.ToLower(strings.TrimSpace(name))),
			attribute.String("tag", strings.ToLower(strings.TrimSpace(tag))),
			attribute.Bool("allowOrphan", allowOrphan),
		))
	}
	return hash, nil
}

// RemoveStrategy deletes the JavaScript strategy file by name.
func (m *Manager) RemoveStrategy(name string) error {
	if m == nil || m.jsLoader == nil {
		return js.ErrModuleNotFound
	}

	selector := strings.TrimSpace(name)
	if selector == "" {
		return fmt.Errorf("strategy remove: selector required")
	}

	var (
		inUseErr error
	)
	if strings.ContainsAny(selector, "@:") {
		resolution, err := m.jsLoader.ResolveReference(selector)
		if err != nil {
			return fmt.Errorf("strategy remove %q: %w", selector, err)
		}
		if resolution.Hash != "" && m.hashInUse(resolution.Hash) {
			inUseErr = fmt.Errorf("strategy revision %s is in use", resolution.Hash)
		}
	} else if m.strategyInUse(selector) {
		inUseErr = fmt.Errorf("strategy %s is in use by running instances", selector)
	}
	if inUseErr != nil {
		return inUseErr
	}

	if err := m.jsLoader.Delete(selector); err != nil {
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

func (m *Manager) strategyInUse(name string) bool {
	if name == "" {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(name))
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, spec := range m.specs {
		if strings.EqualFold(spec.Strategy.Identifier, lower) {
			return true
		}
	}
	return false
}

func (m *Manager) hashInUse(hash string) bool {
	if hash == "" {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(hash))
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, spec := range m.specs {
		if strings.EqualFold(spec.Strategy.Hash, normalized) {
			return true
		}
	}
	return false
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
