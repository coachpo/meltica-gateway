package strategies

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/coachpo/meltica/internal/domain/schema"
)

// MarketMaking implements a simple market making strategy that places buy and sell orders
// around the current mid-price with configurable spreads.
type MarketMaking struct {
	Lambda interface {
		Logger() *log.Logger
		GetMarketState() MarketState
		GetLastPrice() float64
		GetBidPrice() float64
		GetAskPrice() float64
		IsTradingActive() bool
		Providers() []string
		SelectProvider(seed uint64) (string, error)
		SubmitOrder(ctx context.Context, provider string, side schema.TradeSide, quantity string, price *float64) error
		IsDryRun() bool
	}

	// Configuration
	SpreadBps     float64 // Spread in basis points (100 bps = 1%)
	OrderSize     string  // Order size as string
	MaxOpenOrders int32   // Maximum number of open orders per side
	DryRun        bool

	// State
	activeBuyOrders  atomic.Int32
	activeSellOrders atomic.Int32
	lastQuotePrice   atomic.Value // float64
}

var marketMakingSubscribedEvents = []schema.EventType{
	schema.EventTypeTrade,
	schema.EventTypeTicker,
	schema.EventTypeExecReport,
	schema.EventTypeBalanceUpdate,
	schema.EventTypeRiskControl,
}

// SubscribedEvents returns the list of event types this strategy subscribes to.
func (s *MarketMaking) SubscribedEvents() []schema.EventType {
	return append([]schema.EventType(nil), marketMakingSubscribedEvents...)
}

// WantsCrossProviderEvents indicates market making operates on single-provider feeds.
func (s *MarketMaking) WantsCrossProviderEvents() bool {
	return false
}

// MarketState represents the current market snapshot.
type MarketState struct {
	LastPrice float64
	BidPrice  float64
	AskPrice  float64
	Spread    float64
	SpreadPct float64
}

// OnTrade checks if we need to refresh quotes when trades occur.
func (s *MarketMaking) OnTrade(ctx context.Context, _ *schema.Event, _ schema.TradePayload, price float64) {
	if !s.Lambda.IsTradingActive() {
		return
	}

	market := s.Lambda.GetMarketState()
	if market.BidPrice <= 0 || market.AskPrice <= 0 {
		return
	}

	// Check if we need to requote based on price movement
	lastQuote, ok := s.lastQuotePrice.Load().(float64)
	if !ok || lastQuote == 0 {
		s.placeQuotes(ctx, market)
		return
	}

	// Requote if price moved more than half our spread
	priceMove := absFloat(price-lastQuote) / lastQuote * 10000 // in bps
	if priceMove > s.SpreadBps/2 {
		s.Lambda.Logger().Printf("[MM] Price moved %.2f bps, requoting", priceMove)
		s.placeQuotes(ctx, market)
	}
}

// OnTicker updates quotes on ticker updates.
func (s *MarketMaking) OnTicker(ctx context.Context, _ *schema.Event, _ schema.TickerPayload) {
	if !s.Lambda.IsTradingActive() {
		return
	}

	market := s.Lambda.GetMarketState()
	if market.BidPrice <= 0 || market.AskPrice <= 0 {
		return
	}

	s.placeQuotes(ctx, market)
}

// OnBookSnapshot does nothing.
func (s *MarketMaking) OnBookSnapshot(_ context.Context, _ *schema.Event, _ schema.BookSnapshotPayload) {
}

// OnOrderFilled decrements active order count.
func (s *MarketMaking) OnOrderFilled(_ context.Context, _ *schema.Event, payload schema.ExecReportPayload) {
	s.decrementActiveOrders(payload.Side)
	s.Lambda.Logger().Printf("[MM] Order filled: side=%s price=%s qty=%s",
		payload.Side, payload.AvgFillPrice, payload.FilledQuantity)
}

// OnOrderRejected decrements active order count.
func (s *MarketMaking) OnOrderRejected(_ context.Context, _ *schema.Event, payload schema.ExecReportPayload, reason string) {
	s.decrementActiveOrders(payload.Side)
	s.Lambda.Logger().Printf("[MM] Order rejected: side=%s reason=%s", payload.Side, reason)
}

// OnOrderPartialFill logs partial fills.
func (s *MarketMaking) OnOrderPartialFill(_ context.Context, _ *schema.Event, payload schema.ExecReportPayload) {
	s.Lambda.Logger().Printf("[MM] Partial fill: side=%s filled=%s remaining=%s",
		payload.Side, payload.FilledQuantity, payload.RemainingQty)
}

// OnOrderCancelled decrements active order count.
func (s *MarketMaking) OnOrderCancelled(_ context.Context, _ *schema.Event, payload schema.ExecReportPayload) {
	s.decrementActiveOrders(payload.Side)
	s.Lambda.Logger().Printf("[MM] Order cancelled: side=%s", payload.Side)
}

func (s *MarketMaking) placeQuotes(ctx context.Context, market MarketState) {
	provider, err := s.selectProvider()
	if err != nil {
		s.Lambda.Logger().Printf("[MM] Unable to select provider: %v", err)
		return
	}

	midPrice := (market.BidPrice + market.AskPrice) / 2
	if midPrice <= 0 {
		return
	}
	dryRun := s.DryRun || s.Lambda.IsDryRun()

	// Calculate quote prices with configured spread
	spreadMultiplier := s.SpreadBps / 10000.0 // Convert bps to decimal
	buyPrice := midPrice * (1 - spreadMultiplier)
	sellPrice := midPrice * (1 + spreadMultiplier)

	// Place buy order if we have capacity
	if s.activeBuyOrders.Load() < s.MaxOpenOrders {
		if dryRun {
			s.activeBuyOrders.Add(1)
			s.Lambda.Logger().Printf("[MM][DRY-RUN] Would place buy order on %s: price=%.2f size=%s", provider, buyPrice, s.OrderSize)
		} else {
			if err := s.Lambda.SubmitOrder(ctx, provider, schema.TradeSideBuy, s.OrderSize, &buyPrice); err != nil {
				s.Lambda.Logger().Printf("[MM] Failed to submit buy order: %v", err)
			} else {
				s.activeBuyOrders.Add(1)
				s.Lambda.Logger().Printf("[MM] Placed buy order on %s: price=%.2f size=%s", provider, buyPrice, s.OrderSize)
			}
		}
	}

	// Place sell order if we have capacity
	if s.activeSellOrders.Load() < s.MaxOpenOrders {
		if dryRun {
			s.activeSellOrders.Add(1)
			s.Lambda.Logger().Printf("[MM][DRY-RUN] Would place sell order on %s: price=%.2f size=%s", provider, sellPrice, s.OrderSize)
		} else {
			if err := s.Lambda.SubmitOrder(ctx, provider, schema.TradeSideSell, s.OrderSize, &sellPrice); err != nil {
				s.Lambda.Logger().Printf("[MM] Failed to submit sell order: %v", err)
			} else {
				s.activeSellOrders.Add(1)
				s.Lambda.Logger().Printf("[MM] Placed sell order on %s: price=%.2f size=%s", provider, sellPrice, s.OrderSize)
			}
		}
	}

	s.lastQuotePrice.Store(midPrice)
}

func (s *MarketMaking) decrementActiveOrders(side schema.TradeSide) {
	switch side {
	case schema.TradeSideBuy:
		if count := s.activeBuyOrders.Add(-1); count < 0 {
			s.activeBuyOrders.Store(0)
		}
	case schema.TradeSideSell:
		if count := s.activeSellOrders.Add(-1); count < 0 {
			s.activeSellOrders.Store(0)
		}
	}
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// OnOrderAcknowledged tracks acknowledged orders (no-op for this strategy).
func (s *MarketMaking) OnOrderAcknowledged(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
}

// OnOrderExpired tracks expired orders (no-op for this strategy).
func (s *MarketMaking) OnOrderExpired(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
}

// OnKlineSummary tracks kline data (no-op for this strategy).
func (s *MarketMaking) OnKlineSummary(_ context.Context, _ *schema.Event, _ schema.KlineSummaryPayload) {
}

// OnInstrumentUpdate is a no-op for this strategy.
func (s *MarketMaking) OnInstrumentUpdate(_ context.Context, _ *schema.Event, _ schema.InstrumentUpdatePayload) {
}

// OnBalanceUpdate logs balance changes for awareness.
func (s *MarketMaking) OnBalanceUpdate(_ context.Context, _ *schema.Event, payload schema.BalanceUpdatePayload) {
	s.Lambda.Logger().Printf("[MM] Balance update: currency=%s total=%s available=%s",
		payload.Currency, payload.Total, payload.Available)
}

func (s *MarketMaking) selectProvider() (string, error) {
	providers := s.Lambda.Providers()
	if len(providers) == 0 {
		return "", fmt.Errorf("no providers configured")
	}
	// #nosec G115 -- UnixNano used as non-cryptographic seed for provider selection
	provider, err := s.Lambda.SelectProvider(uint64(time.Now().UnixNano()))
	if err == nil && provider != "" {
		return provider, nil
	}
	return providers[0], nil
}

// OnRiskControl resets state when risk controls trigger.
func (s *MarketMaking) OnRiskControl(_ context.Context, _ *schema.Event, payload schema.RiskControlPayload) {
	s.activeBuyOrders.Store(0)
	s.activeSellOrders.Store(0)
	s.lastQuotePrice.Store(float64(0))
	s.Lambda.Logger().Printf("[MM] Risk control notification: status=%s breach=%s reason=%s", payload.Status, payload.BreachType, payload.Reason)
}

// ParseFloat safely parses a string to float64.
func ParseFloat(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse float: %w", err)
	}
	return val, nil
}
