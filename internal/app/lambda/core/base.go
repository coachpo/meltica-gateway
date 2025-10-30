// Package core implements the reusable trading lambda primitives shared across
// Meltica strategies.
package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coachpo/meltica/internal/app/risk"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/bus/eventbus"
	"github.com/coachpo/meltica/internal/infra/pool"
	"github.com/shopspring/decimal"
	"github.com/sourcegraph/conc"
)

// TradingStrategy defines the interface for custom trading logic that can be plugged into a Lambda.
// Implement this interface to create custom trading strategies.
type TradingStrategy interface {
	// Market data callbacks
	OnTrade(ctx context.Context, evt *schema.Event, payload schema.TradePayload, price float64)
	OnTicker(ctx context.Context, evt *schema.Event, payload schema.TickerPayload)
	OnBookSnapshot(ctx context.Context, evt *schema.Event, payload schema.BookSnapshotPayload)
	OnKlineSummary(ctx context.Context, evt *schema.Event, payload schema.KlineSummaryPayload)
	OnInstrumentUpdate(ctx context.Context, evt *schema.Event, payload schema.InstrumentUpdatePayload)
	OnBalanceUpdate(ctx context.Context, evt *schema.Event, payload schema.BalanceUpdatePayload)

	// Order lifecycle callbacks (trading decisions)
	OnOrderFilled(ctx context.Context, evt *schema.Event, payload schema.ExecReportPayload)
	OnOrderRejected(ctx context.Context, evt *schema.Event, payload schema.ExecReportPayload, reason string)
	OnOrderPartialFill(ctx context.Context, evt *schema.Event, payload schema.ExecReportPayload)
	OnOrderCancelled(ctx context.Context, evt *schema.Event, payload schema.ExecReportPayload)

	// Order tracking callbacks (for persistence, auditing, metrics)
	OnOrderAcknowledged(ctx context.Context, evt *schema.Event, payload schema.ExecReportPayload)
	OnOrderExpired(ctx context.Context, evt *schema.Event, payload schema.ExecReportPayload)

	// Risk control notifications
	OnRiskControl(ctx context.Context, evt *schema.Event, payload schema.RiskControlPayload)

	// SubscribedEvents declares the event types the strategy consumes.
	SubscribedEvents() []schema.EventType

	// WantsCrossProviderEvents indicates whether the strategy expects to receive events from
	// multiple providers concurrently.
	WantsCrossProviderEvents() bool
}

// MarketState represents the current market state for a symbol.
type MarketState struct {
	LastPrice float64
	BidPrice  float64
	AskPrice  float64
	Spread    float64
	SpreadPct float64
	UpdatedAt time.Time
}

// BaseLambda provides the core infrastructure for trading lambdas.
// Extend this by embedding it in your custom lambda and providing a TradingStrategy.
type BaseLambda struct {
	id                string
	config            Config
	bus               eventbus.Bus
	orderSubmitter    OrderSubmitter
	pools             *pool.PoolManager
	logger            *log.Logger
	strategy          TradingStrategy
	riskManager       *risk.Manager
	baseCurrency      string
	quoteCurrency     string
	providerSet       map[string]struct{}
	providerSymbols   map[string]map[string]struct{}
	defaultSymbols    map[string]string
	allSymbols        map[string]struct{}
	globalPrimary     string
	balanceCurrencies map[string]struct{}

	// Market state (thread-safe via atomic)
	lastPrice atomic.Value // float64
	bidPrice  atomic.Value // float64
	askPrice  atomic.Value // float64

	// Trading state
	tradingActive atomic.Bool
	orderCount    atomic.Int64
	dryRun        atomic.Bool
}

// Config defines configuration for a lambda trading bot instance.
type Config struct {
	Providers       []string
	ProviderSymbols map[string][]string
	DryRun          bool
}

// OrderSubmitter defines the interface for submitting orders to a provider.
type OrderSubmitter interface {
	SubmitOrder(ctx context.Context, req schema.OrderRequest) error
}

// NewBaseLambda creates a new base lambda with the provided strategy.
func NewBaseLambda(id string, config Config, bus eventbus.Bus, orderSubmitter OrderSubmitter, pools *pool.PoolManager, strategy TradingStrategy, riskManager *risk.Manager) *BaseLambda {
	config.Providers = normalizeProviders(config.Providers)
	config.ProviderSymbols = normalizeProviderSymbols(config.ProviderSymbols)
	providerSet := make(map[string]struct{}, len(config.Providers))
	for _, provider := range config.Providers {
		providerSet[provider] = struct{}{}
	}
	globalPrimary := primarySymbolFromConfig(config.Providers, config.ProviderSymbols)
	if id == "" {
		id = buildLambdaID(config.Providers, config.ProviderSymbols)
	}
	var providerSymbolSets map[string]map[string]struct{}
	defaultSymbols := make(map[string]string, len(config.ProviderSymbols))
	allSymbols := make(map[string]struct{})
	if len(config.ProviderSymbols) > 0 {
		providerSymbolSets = make(map[string]map[string]struct{}, len(config.ProviderSymbols))
		for provider, symbols := range config.ProviderSymbols {
			set := make(map[string]struct{}, len(symbols))
			for _, sym := range symbols {
				set[sym] = struct{}{}
				allSymbols[sym] = struct{}{}
			}
			providerSymbolSets[provider] = set
			if len(symbols) > 0 {
				defaultSymbols[provider] = symbols[0]
			}
		}
		for _, provider := range config.Providers {
			if _, ok := providerSymbolSets[provider]; !ok {
				providerSymbolSets[provider] = make(map[string]struct{})
			}
		}
	}
	if globalPrimary == "" {
		for sym := range allSymbols {
			globalPrimary = sym
			break
		}
	}

	lambda := &BaseLambda{
		id:                id,
		config:            config,
		bus:               bus,
		orderSubmitter:    orderSubmitter,
		pools:             pools,
		logger:            log.New(os.Stdout, "", log.LstdFlags),
		strategy:          strategy,
		riskManager:       riskManager,
		baseCurrency:      "",
		quoteCurrency:     "",
		providerSet:       providerSet,
		providerSymbols:   providerSymbolSets,
		defaultSymbols:    defaultSymbols,
		allSymbols:        allSymbols,
		globalPrimary:     globalPrimary,
		balanceCurrencies: make(map[string]struct{}),
		lastPrice:         atomic.Value{},
		bidPrice:          atomic.Value{},
		askPrice:          atomic.Value{},
		tradingActive:     atomic.Bool{},
		orderCount:        atomic.Int64{},
		dryRun:            atomic.Bool{},
	}

	if lambda.globalPrimary != "" {
		if base, quote, err := schema.InstrumentCurrencies(lambda.globalPrimary); err == nil {
			lambda.baseCurrency = strings.ToUpper(base)
			lambda.quoteCurrency = strings.ToUpper(quote)
		} else {
			lambda.logger.Printf("[%s] unable to derive currencies from symbol %q: %v", lambda.id, lambda.globalPrimary, err)
		}
	}
	for sym := range lambda.allSymbols {
		if base, quote, err := schema.InstrumentCurrencies(sym); err == nil {
			if trimmed := strings.ToUpper(strings.TrimSpace(base)); trimmed != "" {
				lambda.balanceCurrencies[trimmed] = struct{}{}
			}
			if trimmed := strings.ToUpper(strings.TrimSpace(quote)); trimmed != "" {
				lambda.balanceCurrencies[trimmed] = struct{}{}
			}
		}
	}

	lambda.lastPrice.Store(float64(0))
	lambda.bidPrice.Store(float64(0))
	lambda.askPrice.Store(float64(0))
	lambda.tradingActive.Store(false)
	lambda.dryRun.Store(config.DryRun)

	return lambda
}

func buildLambdaID(providers []string, providerSymbols map[string][]string) string {
	primary := strings.TrimSpace(primarySymbolFromConfig(providers, providerSymbols))
	if len(providers) == 0 {
		if primary == "" {
			return "lambda-unassigned"
		}
		return fmt.Sprintf("lambda-%s", primary)
	}
	joined := strings.Join(providers, "-")
	if primary == "" {
		return fmt.Sprintf("lambda-%s", joined)
	}
	return fmt.Sprintf("lambda-%s-%s", primary, joined)
}

func normalizeProviders(providers []string) []string {
	if len(providers) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(providers))
	out := make([]string, 0, len(providers))
	for _, raw := range providers {
		candidate := strings.TrimSpace(raw)
		if candidate == "" {
			continue
		}
		if _, seen := set[candidate]; seen {
			continue
		}
		set[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func normalizeProviderSymbols(input map[string][]string) map[string][]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string][]string, len(input))
	for rawProvider, symbols := range input {
		provider := strings.TrimSpace(rawProvider)
		if provider == "" {
			continue
		}
		if len(symbols) == 0 {
			out[provider] = nil
			continue
		}
		seen := make(map[string]struct{}, len(symbols))
		normalized := make([]string, 0, len(symbols))
		for _, raw := range symbols {
			symbol := strings.ToUpper(strings.TrimSpace(raw))
			if symbol == "" {
				continue
			}
			if _, exists := seen[symbol]; exists {
				continue
			}
			seen[symbol] = struct{}{}
			normalized = append(normalized, symbol)
		}
		if len(normalized) == 0 {
			out[provider] = nil
		} else {
			out[provider] = normalized
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func copyProviderSymbolMap(src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string][]string, len(src))
	for provider, symbols := range src {
		if len(symbols) == 0 {
			dst[provider] = nil
			continue
		}
		copied := make([]string, len(symbols))
		copy(copied, symbols)
		dst[provider] = copied
	}
	return dst
}

func primarySymbolFromConfig(providers []string, providerSymbols map[string][]string) string {
	for _, provider := range providers {
		symbols := providerSymbols[provider]
		if len(symbols) > 0 {
			return symbols[0]
		}
	}
	for _, symbols := range providerSymbols {
		if len(symbols) > 0 {
			return symbols[0]
		}
	}
	return ""
}

// Start begins consuming market data and executing trading logic.
func (l *BaseLambda) Start(ctx context.Context) (<-chan error, error) {
	if l.bus == nil {
		return nil, fmt.Errorf("lambda %s: data bus required", l.id)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	eventTypes := []schema.EventType{
		schema.EventTypeTrade,
		schema.EventTypeTicker,
		schema.EventTypeBookSnapshot,
		schema.EventTypeExecReport,
		schema.EventTypeKlineSummary,
		schema.EventTypeInstrumentUpdate,
		schema.EventTypeBalanceUpdate,
		schema.EventTypeRiskControl,
	}

	errs := make(chan error, len(eventTypes))
	subs := make([]subscription, 0, len(eventTypes))

	for _, typ := range eventTypes {
		subID, ch, err := l.bus.Subscribe(ctx, typ)
		if err != nil {
			close(errs)
			for _, sub := range subs {
				l.bus.Unsubscribe(sub.id)
			}
			return nil, fmt.Errorf("subscribe to %s: %w", typ, err)
		}
		subs = append(subs, subscription{id: subID, typ: typ, ch: ch})
	}

	go l.consume(ctx, subs, errs)

	l.logger.Printf("[%s] started for providers=%v scope=%v", l.id, l.config.Providers, l.config.ProviderSymbols)
	return errs, nil
}

type subscription struct {
	id  eventbus.SubscriptionID
	typ schema.EventType
	ch  <-chan *schema.Event
}

func (l *BaseLambda) consume(ctx context.Context, subs []subscription, errs chan<- error) {
	defer close(errs)

	var wg conc.WaitGroup
	for _, sub := range subs {
		subscription := sub
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					// Drain any buffered events to prevent pool leaks.
					for {
						select {
						case evt, ok := <-subscription.ch:
							if !ok {
								return
							}
							l.recycleEvent(evt)
						default:
							return
						}
					}
				case evt, ok := <-subscription.ch:
					if !ok {
						return
					}
					l.handleEvent(ctx, subscription.typ, evt)
				}
			}
		})
	}

	wg.Wait()

	for _, sub := range subs {
		l.bus.Unsubscribe(sub.id)
	}
}

func (l *BaseLambda) handleEvent(ctx context.Context, typ schema.EventType, evt *schema.Event) {
	if evt == nil {
		return
	}

	defer l.recycleEvent(evt)

	// Filter by provider and symbol
	if !l.matchesProvider(evt) {
		return
	}

	if typ == schema.EventTypeBalanceUpdate {
		if !l.matchesBalanceCurrency(evt.Symbol) {
			return
		}
	} else if !l.matchesSymbol(evt) {
		return
	}

	switch typ {
	case schema.EventTypeTrade:
		l.handleTrade(ctx, evt)
	case schema.EventTypeTicker:
		l.handleTicker(ctx, evt)
	case schema.EventTypeBookSnapshot:
		l.handleBookSnapshot(ctx, evt)
	case schema.EventTypeExecReport:
		l.handleExecReport(ctx, evt)
	case schema.EventTypeKlineSummary:
		l.handleKlineSummary(ctx, evt)
	case schema.EventTypeInstrumentUpdate:
		l.handleInstrumentUpdate(ctx, evt)
	case schema.EventTypeBalanceUpdate:
		l.handleBalanceUpdate(ctx, evt)
	case schema.EventTypeRiskControl:
		l.handleRiskControl(ctx, evt)
	}
}

func (l *BaseLambda) handleTrade(ctx context.Context, evt *schema.Event) {
	payload, ok := evt.Payload.(schema.TradePayload)
	if !ok {
		return
	}

	price, err := strconv.ParseFloat(payload.Price, 64)
	if err != nil {
		return
	}

	l.lastPrice.Store(price)
	if l.riskManager != nil {
		if decPrice, convErr := decimal.NewFromString(payload.Price); convErr == nil {
			l.riskManager.ObserveMarketPrice(evt.Symbol, decPrice)
		}
	}

	if l.strategy != nil {
		l.strategy.OnTrade(ctx, evt, payload, price)
	}
}

func (l *BaseLambda) handleTicker(ctx context.Context, evt *schema.Event) {
	payload, ok := evt.Payload.(schema.TickerPayload)
	if !ok {
		return
	}

	lastPrice, _ := strconv.ParseFloat(payload.LastPrice, 64)
	bidPrice, _ := strconv.ParseFloat(payload.BidPrice, 64)
	askPrice, _ := strconv.ParseFloat(payload.AskPrice, 64)

	l.lastPrice.Store(lastPrice)
	l.bidPrice.Store(bidPrice)
	l.askPrice.Store(askPrice)
	if l.riskManager != nil {
		if decPrice, convErr := decimal.NewFromString(payload.LastPrice); convErr == nil {
			l.riskManager.ObserveMarketPrice(evt.Symbol, decPrice)
		}
	}

	if l.strategy != nil {
		l.strategy.OnTicker(ctx, evt, payload)
	}
}

func (l *BaseLambda) handleBookSnapshot(ctx context.Context, evt *schema.Event) {
	payload, ok := evt.Payload.(schema.BookSnapshotPayload)
	if !ok {
		return
	}

	if len(payload.Bids) > 0 {
		bidPrice, _ := strconv.ParseFloat(payload.Bids[0].Price, 64)
		l.bidPrice.Store(bidPrice)
	}

	if len(payload.Asks) > 0 {
		askPrice, _ := strconv.ParseFloat(payload.Asks[0].Price, 64)
		l.askPrice.Store(askPrice)
	}

	if l.riskManager != nil {
		if len(payload.Bids) > 0 && len(payload.Asks) > 0 {
			if bidDec, errBid := decimal.NewFromString(payload.Bids[0].Price); errBid == nil {
				if askDec, errAsk := decimal.NewFromString(payload.Asks[0].Price); errAsk == nil {
					mid := bidDec.Add(askDec).Div(decimal.NewFromInt(2))
					l.riskManager.ObserveMarketPrice(evt.Symbol, mid)
				}
			}
		}
	}

	if l.strategy != nil {
		l.strategy.OnBookSnapshot(ctx, evt, payload)
	}
}

func (l *BaseLambda) handleExecReport(ctx context.Context, evt *schema.Event) {
	payload, ok := evt.Payload.(schema.ExecReportPayload)
	if !ok {
		return
	}

	// Only process ExecReports for orders submitted by this lambda
	if !l.IsMyOrder(payload.ClientOrderID) {
		return
	}

	if l.riskManager != nil {
		l.riskManager.HandleExecution(evt.Symbol, payload)
	}

	// Delegate to strategy based on state
	if l.strategy == nil {
		return
	}

	switch payload.State {
	case schema.ExecReportStateFILLED:
		l.strategy.OnOrderFilled(ctx, evt, payload)

	case schema.ExecReportStateREJECTED:
		reason := ""
		if payload.RejectReason != nil {
			reason = *payload.RejectReason
		}
		l.strategy.OnOrderRejected(ctx, evt, payload, reason)

	case schema.ExecReportStatePARTIAL:
		l.strategy.OnOrderPartialFill(ctx, evt, payload)

	case schema.ExecReportStateCANCELLED:
		l.strategy.OnOrderCancelled(ctx, evt, payload)

	case schema.ExecReportStateACK:
		// Order acknowledged by exchange - useful for persistence, auditing, reconciliation
		l.strategy.OnOrderAcknowledged(ctx, evt, payload)

	case schema.ExecReportStateEXPIRED:
		// Order expired (e.g., GTD orders that reached time limit)
		// Useful for tracking order lifecycle, metrics, compliance
		l.strategy.OnOrderExpired(ctx, evt, payload)
	}
}

func (l *BaseLambda) handleInstrumentUpdate(ctx context.Context, evt *schema.Event) {
	if l.strategy == nil {
		return
	}
	payload, ok := evt.Payload.(schema.InstrumentUpdatePayload)
	if !ok {
		return
	}
	l.strategy.OnInstrumentUpdate(ctx, evt, payload)
}

func (l *BaseLambda) handleKlineSummary(ctx context.Context, evt *schema.Event) {
	payload, ok := evt.Payload.(schema.KlineSummaryPayload)
	if !ok {
		return
	}

	if l.strategy != nil {
		l.strategy.OnKlineSummary(ctx, evt, payload)
	}
}

func (l *BaseLambda) handleBalanceUpdate(ctx context.Context, evt *schema.Event) {
	if l.strategy == nil {
		return
	}
	payload, ok := evt.Payload.(schema.BalanceUpdatePayload)
	if !ok {
		return
	}
	l.strategy.OnBalanceUpdate(ctx, evt, payload)
}

func (l *BaseLambda) handleRiskControl(ctx context.Context, evt *schema.Event) {
	if l.strategy == nil {
		return
	}
	var payload schema.RiskControlPayload
	switch v := evt.Payload.(type) {
	case schema.RiskControlPayload:
		payload = v
	case *schema.RiskControlPayload:
		if v == nil {
			return
		}
		payload = *v
	default:
		return
	}
	if payload.Provider == "" {
		payload.Provider = evt.Provider
	}
	if payload.Symbol == "" {
		payload.Symbol = evt.Symbol
	}
	l.strategy.OnRiskControl(ctx, evt, payload)
}

// SubmitOrder submits an order request to the specified provider.
func (l *BaseLambda) SubmitOrder(ctx context.Context, provider string, side schema.TradeSide, quantity string, price *string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("order provider required")
	}
	if len(l.config.Providers) > 0 {
		if _, ok := l.providerSet[provider]; !ok {
			return fmt.Errorf("order provider %q not configured for lambda %s", provider, l.id)
		}
	}

	if l.IsDryRun() {
		var priceStr string
		if price != nil {
			priceStr = *price
		} else {
			priceStr = "market"
		}
		l.logger.Printf("[%s] dry-run: skip submit order provider=%s side=%s qty=%s price=%s", l.id, provider, side, quantity, priceStr)
		return nil
	}

	if l.orderSubmitter == nil {
		return fmt.Errorf("order submitter not configured")
	}
	if l.pools == nil {
		return fmt.Errorf("pool manager not configured")
	}

	orderID := fmt.Sprintf("%s-%d-%d", l.id, time.Now().UnixNano(), l.orderCount.Load())

	orderReq, release, err := pool.AcquireOrderRequest(ctx, l.pools)
	if err != nil {
		return fmt.Errorf("acquire order request from pool: %w", err)
	}
	defer release()

	orderReq.ClientOrderID = orderID
	orderReq.ConsumerID = l.id
	orderReq.Provider = provider
	orderReq.Symbol = l.symbolForProvider(provider)
	orderReq.Side = side
	orderReq.OrderType = schema.OrderTypeLimit
	orderReq.Price = price
	orderReq.Quantity = quantity
	orderReq.TIF = "GTC"
	orderReq.Timestamp = time.Now().UTC()

	if l.riskManager != nil {
		if err := l.riskManager.CheckOrder(ctx, orderReq); err != nil {
			l.emitRiskControlEvent(ctx, l.buildRiskControlPayload(provider, err))
			return fmt.Errorf("risk check failed: %w", err)
		}
	}

	if err := l.orderSubmitter.SubmitOrder(ctx, *orderReq); err != nil {
		return fmt.Errorf("submit order: %w", err)
	}

	l.orderCount.Add(1)
	return nil
}

// SubmitMarketOrder submits a market order.
func (l *BaseLambda) SubmitMarketOrder(ctx context.Context, provider string, side schema.TradeSide, quantity string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("order provider required")
	}
	if len(l.config.Providers) > 0 {
		if _, ok := l.providerSet[provider]; !ok {
			return fmt.Errorf("order provider %q not configured for lambda %s", provider, l.id)
		}
	}

	if l.IsDryRun() {
		l.logger.Printf("[%s] dry-run: skip submit market order provider=%s side=%s qty=%s", l.id, provider, side, quantity)
		return nil
	}

	if l.orderSubmitter == nil {
		return fmt.Errorf("order submitter not configured")
	}
	if l.pools == nil {
		return fmt.Errorf("pool manager not configured")
	}

	orderID := fmt.Sprintf("%s-%d-%d", l.id, time.Now().UnixNano(), l.orderCount.Load())

	orderReq, release, err := pool.AcquireOrderRequest(ctx, l.pools)
	if err != nil {
		return fmt.Errorf("acquire order request from pool: %w", err)
	}
	defer release()

	orderReq.ClientOrderID = orderID
	orderReq.ConsumerID = l.id
	orderReq.Provider = provider
	orderReq.Symbol = l.symbolForProvider(provider)
	orderReq.Side = side
	orderReq.OrderType = schema.OrderTypeMarket
	orderReq.Quantity = quantity
	orderReq.TIF = "IOC"
	orderReq.Timestamp = time.Now().UTC()

	if l.riskManager != nil {
		if err := l.riskManager.CheckOrder(ctx, orderReq); err != nil {
			l.emitRiskControlEvent(ctx, l.buildRiskControlPayload(provider, err))
			return fmt.Errorf("risk check failed: %w", err)
		}
	}

	if err := l.orderSubmitter.SubmitOrder(ctx, *orderReq); err != nil {
		return fmt.Errorf("submit market order: %w", err)
	}

	l.orderCount.Add(1)
	return nil
}

// Protected accessor methods for subclasses

// ID returns the lambda instance ID.
func (l *BaseLambda) ID() string {
	return l.id
}

// Config returns the lambda configuration.
func (l *BaseLambda) Config() Config {
	copyCfg := Config{
		Providers:       make([]string, len(l.config.Providers)),
		ProviderSymbols: copyProviderSymbolMap(l.config.ProviderSymbols),
		DryRun:          l.config.DryRun,
	}
	copy(copyCfg.Providers, l.config.Providers)
	return copyCfg
}

// Logger returns the logger instance.
func (l *BaseLambda) Logger() *log.Logger {
	return l.logger
}

// GetMarketState returns the current market state.
func (l *BaseLambda) GetMarketState() MarketState {
	lastPrice := l.lastPrice.Load().(float64)
	bidPrice := l.bidPrice.Load().(float64)
	askPrice := l.askPrice.Load().(float64)

	spread := askPrice - bidPrice
	spreadPct := float64(0)
	if bidPrice > 0 {
		spreadPct = (spread / bidPrice) * 100
	}

	return MarketState{
		LastPrice: lastPrice,
		BidPrice:  bidPrice,
		AskPrice:  askPrice,
		Spread:    spread,
		SpreadPct: spreadPct,
		UpdatedAt: time.Now(),
	}
}

// GetLastPrice returns the last traded price.
func (l *BaseLambda) GetLastPrice() float64 {
	return l.lastPrice.Load().(float64)
}

// GetBidPrice returns the current best bid price.
func (l *BaseLambda) GetBidPrice() float64 {
	return l.bidPrice.Load().(float64)
}

// GetAskPrice returns the current best ask price.
func (l *BaseLambda) GetAskPrice() float64 {
	return l.askPrice.Load().(float64)
}

// GetSpread returns the current bid-ask spread.
func (l *BaseLambda) GetSpread() float64 {
	bid := l.bidPrice.Load().(float64)
	ask := l.askPrice.Load().(float64)
	return ask - bid
}

// GetSpreadPercent returns the spread as a percentage of the bid price.
func (l *BaseLambda) GetSpreadPercent() float64 {
	bid := l.bidPrice.Load().(float64)
	if bid <= 0 {
		return 0
	}
	spread := l.GetSpread()
	return (spread / bid) * 100
}

// GetOrderCount returns the total number of orders submitted.
func (l *BaseLambda) GetOrderCount() int64 {
	return l.orderCount.Load()
}

// Providers returns the configured provider list.
func (l *BaseLambda) Providers() []string {
	providers := make([]string, len(l.config.Providers))
	copy(providers, l.config.Providers)
	return providers
}

func (l *BaseLambda) symbolForProvider(provider string) string {
	provider = strings.TrimSpace(provider)
	if provider != "" {
		if sym, ok := l.defaultSymbols[provider]; ok && sym != "" {
			return sym
		}
	}
	if l.globalPrimary != "" {
		return l.globalPrimary
	}
	for sym := range l.allSymbols {
		return sym
	}
	return ""
}

// SelectProvider chooses a provider based on the supplied seed.
func (l *BaseLambda) SelectProvider(seed uint64) (string, error) {
	if len(l.config.Providers) == 0 {
		return "", fmt.Errorf("no providers configured")
	}
	idx := int(seed % uint64(len(l.config.Providers))) // #nosec G115 -- modulo result is always within int range for provider slice length
	return l.config.Providers[idx], nil
}

// IsTradingActive returns whether trading is currently enabled.
func (l *BaseLambda) IsTradingActive() bool {
	return l.tradingActive.Load()
}

// IsDryRun reports whether the lambda is operating in dry-run mode.
func (l *BaseLambda) IsDryRun() bool {
	return l.dryRun.Load()
}

// EnableTrading enables or disables trading for this lambda instance.
func (l *BaseLambda) EnableTrading(enabled bool) {
	l.tradingActive.Store(enabled)
	status := "DISABLED"
	if enabled {
		status = "ENABLED"
	}
	l.logger.Printf("[%s] Trading %s", l.id, status)
}

// IsMyOrder checks if the ClientOrderID belongs to this lambda instance.
// Order IDs are formatted as: "{lambda-id}-{timestamp}-{count}"
func (l *BaseLambda) IsMyOrder(clientOrderID string) bool {
	if clientOrderID == "" {
		return false
	}
	prefix := l.id + "-"
	return len(clientOrderID) > len(prefix) && clientOrderID[:len(prefix)] == prefix
}

// Private helper methods

func (l *BaseLambda) matchesSymbol(evt *schema.Event) bool {
	if evt == nil {
		return false
	}
	symbol := strings.ToUpper(strings.TrimSpace(evt.Symbol))
	if symbol == "" {
		return false
	}
	if len(l.providerSymbols) == 0 {
		if len(l.allSymbols) == 0 {
			return true
		}
		_, ok := l.allSymbols[symbol]
		return ok
	}
	provider := strings.TrimSpace(evt.Provider)
	if provider == "" {
		return false
	}
	allowed, ok := l.providerSymbols[provider]
	if !ok || len(allowed) == 0 {
		return false
	}
	_, ok = allowed[symbol]
	return ok
}

func (l *BaseLambda) matchesProvider(evt *schema.Event) bool {
	if len(l.config.Providers) == 0 {
		return true
	}
	if evt == nil {
		return false
	}
	_, ok := l.providerSet[evt.Provider]
	return ok
}

func (l *BaseLambda) recycleEvent(evt *schema.Event) {
	if evt == nil {
		return
	}
	if l.pools != nil {
		if ok := l.pools.TryReturnEventInst(evt); !ok {
			// TryReturnEventInst returns false when the pool already reclaimed the object.
			// Avoid panicking on double puts—log at debug level instead.
			if l.logger != nil {
				l.logger.Printf("[%s] skipping double return for event %s from pool", l.id, evt.EventID)
			}
		}
	}
}

func (l *BaseLambda) matchesBalanceCurrency(symbol string) bool {
	currency := strings.ToUpper(strings.TrimSpace(symbol))
	if currency == "" {
		return false
	}
	if l.baseCurrency != "" && currency == l.baseCurrency {
		return true
	}
	if l.quoteCurrency != "" && currency == l.quoteCurrency {
		return true
	}
	if len(l.balanceCurrencies) > 0 {
		_, ok := l.balanceCurrencies[currency]
		return ok
	}
	return false
}

func (l *BaseLambda) buildRiskControlPayload(provider string, err error) schema.RiskControlPayload {
	provider = strings.TrimSpace(provider)
	if provider == "" && len(l.config.Providers) == 1 {
		provider = l.config.Providers[0]
	}
	primarySymbol := l.symbolForProvider(provider)
	payload := schema.RiskControlPayload{
		StrategyID:         l.id,
		Provider:           provider,
		Symbol:             primarySymbol,
		Status:             schema.RiskControlStatusTriggered,
		Reason:             err.Error(),
		BreachType:         "UNKNOWN",
		Metrics:            nil,
		KillSwitchEngaged:  false,
		CircuitBreakerOpen: false,
		Timestamp:          time.Now().UTC(),
	}

	var breach *risk.BreachError
	if errors.As(err, &breach) && breach != nil {
		payload.Reason = breach.Reason
		payload.BreachType = string(breach.Type)
		if len(breach.Details) > 0 {
			metrics := make(map[string]string, len(breach.Details))
			for k, v := range breach.Details {
				metrics[k] = v
			}
			payload.Metrics = metrics
		}
		payload.KillSwitchEngaged = breach.KillSwitchEngaged
		payload.CircuitBreakerOpen = breach.CircuitBreakerOpen
	} else if errors.Is(err, risk.ErrKillSwitchEngaged) {
		payload.BreachType = string(risk.BreachTypeKillSwitch)
		payload.KillSwitchEngaged = true
		payload.Reason = "kill switch engaged"
	} else if errors.Is(err, risk.ErrCircuitBreakerOpen) {
		payload.BreachType = string(risk.BreachTypeKillSwitch)
		payload.KillSwitchEngaged = true
		payload.CircuitBreakerOpen = true
		payload.Reason = "circuit breaker open"
	}

	return payload
}

func (l *BaseLambda) emitRiskControlEvent(ctx context.Context, payload schema.RiskControlPayload) {
	if l.bus == nil {
		return
	}
	if payload.Timestamp.IsZero() {
		payload.Timestamp = time.Now().UTC()
	}

	if l.pools == nil {
		l.logger.Printf("[%s] risk control event skipped: event pool unavailable", l.id)
		return
	}

	evt, err := l.pools.BorrowEventInst(ctx)
	if err != nil {
		l.logger.Printf("[%s] unable to borrow event from pool: %v", l.id, err)
		return
	}
	evt.EventID = fmt.Sprintf("risk:%s:%d", l.id, payload.Timestamp.UnixNano())
	if payload.Provider != "" {
		evt.Provider = payload.Provider
	} else if len(l.config.Providers) == 1 {
		evt.Provider = l.config.Providers[0]
	}
	if payload.Symbol != "" {
		evt.Symbol = payload.Symbol
	} else {
		evt.Symbol = l.symbolForProvider(evt.Provider)
	}
	evt.Type = schema.EventTypeRiskControl
	evt.IngestTS = payload.Timestamp
	evt.EmitTS = payload.Timestamp
	evt.Payload = payload

	if err := l.bus.Publish(ctx, evt); err != nil {
		l.logger.Printf("[%s] publish risk control event: %v", l.id, err)
		if l.pools != nil {
			l.pools.ReturnEventInst(evt)
		}
	}
}
