// Package lambda implements trading lambdas that process market data events and execute trading logic.
package lambda

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coachpo/meltica/internal/bus/eventbus"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
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

	// SubscribedEvents declares the event types the strategy consumes.
	SubscribedEvents() []schema.EventType
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
	id             string
	config         Config
	bus            eventbus.Bus
	orderSubmitter OrderSubmitter
	pools          *pool.PoolManager
	logger         *log.Logger
	strategy       TradingStrategy
	baseCurrency   string
	quoteCurrency  string

	// Market state (thread-safe via atomic)
	lastPrice atomic.Value // float64
	bidPrice  atomic.Value // float64
	askPrice  atomic.Value // float64

	// Trading state
	tradingActive atomic.Bool
	orderCount    atomic.Int64
}

// Config defines configuration for a lambda trading bot instance.
type Config struct {
	Symbol   string
	Provider string
}

// OrderSubmitter defines the interface for submitting orders to a provider.
type OrderSubmitter interface {
	SubmitOrder(ctx context.Context, req schema.OrderRequest) error
}

// NewBaseLambda creates a new base lambda with the provided strategy.
func NewBaseLambda(id string, config Config, bus eventbus.Bus, orderSubmitter OrderSubmitter, pools *pool.PoolManager, strategy TradingStrategy) *BaseLambda {
	if id == "" {
		id = fmt.Sprintf("lambda-%s-%s", config.Symbol, config.Provider)
	}

	lambda := &BaseLambda{
		id:             id,
		config:         config,
		bus:            bus,
		orderSubmitter: orderSubmitter,
		pools:          pools,
		logger:         log.New(os.Stdout, "", log.LstdFlags),
		strategy:       strategy,
		baseCurrency:   "",
		quoteCurrency:  "",
		lastPrice:      atomic.Value{},
		bidPrice:       atomic.Value{},
		askPrice:       atomic.Value{},
		tradingActive:  atomic.Bool{},
		orderCount:     atomic.Int64{},
	}

	if base, quote, err := schema.InstrumentCurrencies(config.Symbol); err == nil {
		lambda.baseCurrency = strings.ToUpper(base)
		lambda.quoteCurrency = strings.ToUpper(quote)
	} else if config.Symbol != "" {
		lambda.logger.Printf("[%s] unable to derive currencies from symbol %q: %v", lambda.id, config.Symbol, err)
	}

	lambda.lastPrice.Store(float64(0))
	lambda.bidPrice.Store(float64(0))
	lambda.askPrice.Store(float64(0))
	lambda.tradingActive.Store(false)

	return lambda
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
		schema.EventTypeInstrumentUpdate,
		schema.EventTypeBalanceUpdate,
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

	l.logger.Printf("[%s] started for symbol=%s provider=%s", l.id, l.config.Symbol, l.config.Provider)
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
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case evt, ok := <-sub.ch:
					if !ok {
						return
					}
					l.handleEvent(ctx, sub.typ, evt)
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

// SubmitOrder submits an order request to the provider.
func (l *BaseLambda) SubmitOrder(ctx context.Context, side schema.TradeSide, quantity string, price *string) error {
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
	orderReq.Provider = l.config.Provider
	orderReq.Symbol = l.config.Symbol
	orderReq.Side = side
	orderReq.OrderType = schema.OrderTypeLimit
	orderReq.Price = price
	orderReq.Quantity = quantity
	orderReq.TIF = "GTC"
	orderReq.Timestamp = time.Now().UTC()

	if err := l.orderSubmitter.SubmitOrder(ctx, *orderReq); err != nil {
		return fmt.Errorf("submit order: %w", err)
	}

	l.orderCount.Add(1)
	return nil
}

// SubmitMarketOrder submits a market order.
func (l *BaseLambda) SubmitMarketOrder(ctx context.Context, side schema.TradeSide, quantity string) error {
	if l.orderSubmitter == nil {
		return fmt.Errorf("order submitter not configured")
	}
	if l.pools == nil {
		return fmt.Errorf("pool manager not configured")
	}

	orderID := fmt.Sprintf("%s-%d-%d", l.id, time.Now().UnixNano(), l.orderCount.Add(1))

	orderReq, release, err := pool.AcquireOrderRequest(ctx, l.pools)
	if err != nil {
		return fmt.Errorf("acquire order request from pool: %w", err)
	}
	defer release()

	orderReq.ClientOrderID = orderID
	orderReq.ConsumerID = l.id
	orderReq.Provider = l.config.Provider
	orderReq.Symbol = l.config.Symbol
	orderReq.Side = side
	orderReq.OrderType = schema.OrderTypeMarket
	orderReq.Quantity = quantity
	orderReq.TIF = "IOC"
	orderReq.Timestamp = time.Now().UTC()

	if err := l.orderSubmitter.SubmitOrder(ctx, *orderReq); err != nil {
		return fmt.Errorf("submit market order: %w", err)
	}

	return nil
}

// Protected accessor methods for subclasses

// ID returns the lambda instance ID.
func (l *BaseLambda) ID() string {
	return l.id
}

// Config returns the lambda configuration.
func (l *BaseLambda) Config() Config {
	return l.config
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

// IsTradingActive returns whether trading is currently enabled.
func (l *BaseLambda) IsTradingActive() bool {
	return l.tradingActive.Load()
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
	return evt.Symbol == l.config.Symbol
}

func (l *BaseLambda) matchesProvider(evt *schema.Event) bool {
	if l.config.Provider == "" {
		return true
	}
	return evt.Provider == l.config.Provider
}

func (l *BaseLambda) recycleEvent(evt *schema.Event) {
	if evt == nil {
		return
	}
	if l.pools != nil {
		l.pools.ReturnEventInst(evt)
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
	return false
}
