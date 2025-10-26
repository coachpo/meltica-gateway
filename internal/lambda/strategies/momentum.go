package strategies

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

// Momentum implements a momentum trading strategy that detects price trends
// and trades in the direction of the momentum.
type Momentum struct {
	Lambda interface {
		Logger() *log.Logger
		GetLastPrice() float64
		IsTradingActive() bool
		Providers() []string
		SelectProvider(seed uint64) (string, error)
		SubmitMarketOrder(ctx context.Context, provider string, side schema.TradeSide, quantity string) error
	}

	// Configuration
	LookbackPeriod    int           // Number of trades to analyze
	MomentumThreshold float64       // Minimum price change % to trigger trade
	OrderSize         string        // Order size as string
	Cooldown          time.Duration // Minimum time between trades

	// State
	mu            sync.Mutex
	priceHistory  []pricePoint
	lastTradeTime time.Time
	position      int32 // 1 = long, -1 = short, 0 = flat
}

var momentumSubscribedEvents = []schema.EventType{
	schema.EventTypeTrade,
	schema.EventTypeExecReport,
	schema.EventTypeBalanceUpdate,
	schema.EventTypeRiskControl,
}

// SubscribedEvents returns the list of event types this strategy subscribes to.
func (s *Momentum) SubscribedEvents() []schema.EventType {
	return append([]schema.EventType(nil), momentumSubscribedEvents...)
}

// WantsCrossProviderEvents indicates momentum strategies operate on single-provider feeds.
func (s *Momentum) WantsCrossProviderEvents() bool {
	return false
}

type pricePoint struct {
	Price     float64
	Timestamp time.Time
}

// OnTrade analyzes momentum and places trades.
func (s *Momentum) OnTrade(ctx context.Context, _ *schema.Event, _ schema.TradePayload, price float64) {
	if !s.Lambda.IsTradingActive() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Add to price history
	s.priceHistory = append(s.priceHistory, pricePoint{
		Price:     price,
		Timestamp: time.Now(),
	})

	// Keep only lookback period
	if len(s.priceHistory) > s.LookbackPeriod {
		s.priceHistory = s.priceHistory[len(s.priceHistory)-s.LookbackPeriod:]
	}

	// Need enough history
	if len(s.priceHistory) < s.LookbackPeriod {
		return
	}

	// Check cooldown
	if time.Since(s.lastTradeTime) < s.Cooldown {
		return
	}

	// Calculate momentum
	momentum := s.calculateMomentum()
	momentumPct := momentum * 100

	s.Lambda.Logger().Printf("[MOMENTUM] Current momentum: %.3f%%", momentumPct)

	// Strong upward momentum - go long
	if momentumPct > s.MomentumThreshold && s.position <= 0 {
		provider, err := s.selectProvider()
		if err != nil {
			s.Lambda.Logger().Printf("[MOMENTUM] No provider available for buy: %v", err)
			return
		}
		if err := s.Lambda.SubmitMarketOrder(ctx, provider, schema.TradeSideBuy, s.OrderSize); err != nil {
			s.Lambda.Logger().Printf("[MOMENTUM] Failed to buy: %v", err)
		} else {
			s.Lambda.Logger().Printf("[MOMENTUM] BUY signal on %s: momentum=%.3f%%", provider, momentumPct)
			s.position = 1
			s.lastTradeTime = time.Now()
		}
	}

	// Strong downward momentum - go short
	if momentumPct < -s.MomentumThreshold && s.position >= 0 {
		provider, err := s.selectProvider()
		if err != nil {
			s.Lambda.Logger().Printf("[MOMENTUM] No provider available for sell: %v", err)
			return
		}
		if err := s.Lambda.SubmitMarketOrder(ctx, provider, schema.TradeSideSell, s.OrderSize); err != nil {
			s.Lambda.Logger().Printf("[MOMENTUM] Failed to sell: %v", err)
		} else {
			s.Lambda.Logger().Printf("[MOMENTUM] SELL signal on %s: momentum=%.3f%%", provider, momentumPct)
			s.position = -1
			s.lastTradeTime = time.Now()
		}
	}
}

// OnTicker does nothing.
func (s *Momentum) OnTicker(_ context.Context, _ *schema.Event, _ schema.TickerPayload) {}

// OnBookSnapshot does nothing.
func (s *Momentum) OnBookSnapshot(_ context.Context, _ *schema.Event, _ schema.BookSnapshotPayload) {}

// OnOrderFilled logs fills.
func (s *Momentum) OnOrderFilled(_ context.Context, _ *schema.Event, payload schema.ExecReportPayload) {
	s.Lambda.Logger().Printf("[MOMENTUM] Order filled: side=%s price=%s qty=%s",
		payload.Side, payload.AvgFillPrice, payload.FilledQuantity)
}

// OnOrderRejected logs rejections and resets position.
func (s *Momentum) OnOrderRejected(_ context.Context, _ *schema.Event, payload schema.ExecReportPayload, reason string) {
	s.mu.Lock()
	s.position = 0
	s.mu.Unlock()
	s.Lambda.Logger().Printf("[MOMENTUM] Order rejected: side=%s reason=%s", payload.Side, reason)
}

// OnOrderPartialFill logs partial fills.
func (s *Momentum) OnOrderPartialFill(_ context.Context, _ *schema.Event, payload schema.ExecReportPayload) {
	s.Lambda.Logger().Printf("[MOMENTUM] Partial fill: side=%s filled=%s",
		payload.Side, payload.FilledQuantity)
}

// OnOrderCancelled logs cancellations.
func (s *Momentum) OnOrderCancelled(_ context.Context, _ *schema.Event, payload schema.ExecReportPayload) {
	s.Lambda.Logger().Printf("[MOMENTUM] Order cancelled: side=%s", payload.Side)
}

// OnOrderAcknowledged tracks acknowledged orders (no-op for this strategy).
func (s *Momentum) OnOrderAcknowledged(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
}

// OnOrderExpired tracks expired orders (no-op for this strategy).
func (s *Momentum) OnOrderExpired(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {}

// OnKlineSummary tracks kline data (no-op for this strategy).
func (s *Momentum) OnKlineSummary(_ context.Context, _ *schema.Event, _ schema.KlineSummaryPayload) {}

// OnInstrumentUpdate is a no-op for this strategy.
func (s *Momentum) OnInstrumentUpdate(_ context.Context, _ *schema.Event, _ schema.InstrumentUpdatePayload) {
}

// OnBalanceUpdate logs balance updates to track available capital.
func (s *Momentum) OnBalanceUpdate(_ context.Context, _ *schema.Event, payload schema.BalanceUpdatePayload) {
	s.Lambda.Logger().Printf("[MOMENTUM] Balance update: currency=%s total=%s available=%s",
		payload.Currency, payload.Total, payload.Available)
}

// OnRiskControl resets momentum state in response to risk notifications.
func (s *Momentum) OnRiskControl(_ context.Context, _ *schema.Event, payload schema.RiskControlPayload) {
	s.mu.Lock()
	s.position = 0
	s.mu.Unlock()
	s.Lambda.Logger().Printf("[MOMENTUM] Risk control notification: status=%s breach=%s reason=%s", payload.Status, payload.BreachType, payload.Reason)
}

// calculateMomentum returns the price change ratio over the lookback period.
func (s *Momentum) calculateMomentum() float64 {
	if len(s.priceHistory) < 2 {
		return 0
	}

	firstPrice := s.priceHistory[0].Price
	lastPrice := s.priceHistory[len(s.priceHistory)-1].Price

	if firstPrice == 0 {
		return 0
	}

	return (lastPrice - firstPrice) / firstPrice
}

func (s *Momentum) selectProvider() (string, error) {
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
