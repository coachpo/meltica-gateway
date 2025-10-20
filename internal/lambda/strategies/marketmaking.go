package strategies

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync/atomic"

	"github.com/coachpo/meltica/internal/schema"
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
		SubmitOrder(ctx context.Context, side schema.TradeSide, quantity string, price *float64) error
	}

	// Configuration
	SpreadBps     float64 // Spread in basis points (100 bps = 1%)
	OrderSize     string  // Order size as string
	MaxOpenOrders int32   // Maximum number of open orders per side

	// State
	activeBuyOrders  atomic.Int32
	activeSellOrders atomic.Int32
	lastQuotePrice   atomic.Value // float64
}

var marketMakingSubscribedEvents = []schema.CanonicalType{
	schema.CanonicalType("TRADE"),
	schema.CanonicalType("TICKER"),
	schema.CanonicalType("EXECUTION.REPORT"),
}

// SubscribedEvents returns the list of event types this strategy subscribes to.
func (s *MarketMaking) SubscribedEvents() []schema.CanonicalType {
	return append([]schema.CanonicalType(nil), marketMakingSubscribedEvents...)
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
	midPrice := (market.BidPrice + market.AskPrice) / 2
	if midPrice <= 0 {
		return
	}

	// Calculate quote prices with configured spread
	spreadMultiplier := s.SpreadBps / 10000.0 // Convert bps to decimal
	buyPrice := midPrice * (1 - spreadMultiplier)
	sellPrice := midPrice * (1 + spreadMultiplier)

	// Place buy order if we have capacity
	if s.activeBuyOrders.Load() < s.MaxOpenOrders {
		if err := s.Lambda.SubmitOrder(ctx, schema.TradeSideBuy, s.OrderSize, &buyPrice); err != nil {
			s.Lambda.Logger().Printf("[MM] Failed to submit buy order: %v", err)
		} else {
			s.activeBuyOrders.Add(1)
			s.Lambda.Logger().Printf("[MM] Placed buy order: price=%.2f size=%s", buyPrice, s.OrderSize)
		}
	}

	// Place sell order if we have capacity
	if s.activeSellOrders.Load() < s.MaxOpenOrders {
		if err := s.Lambda.SubmitOrder(ctx, schema.TradeSideSell, s.OrderSize, &sellPrice); err != nil {
			s.Lambda.Logger().Printf("[MM] Failed to submit sell order: %v", err)
		} else {
			s.activeSellOrders.Add(1)
			s.Lambda.Logger().Printf("[MM] Placed sell order: price=%.2f size=%s", sellPrice, s.OrderSize)
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

// OnControlAck tracks control acknowledgments (no-op for this strategy).
func (s *MarketMaking) OnControlAck(_ context.Context, _ *schema.Event, _ schema.ControlAckPayload) {}

// OnControlResult tracks control results (no-op for this strategy).
func (s *MarketMaking) OnControlResult(_ context.Context, _ *schema.Event, _ schema.ControlResultPayload) {
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
