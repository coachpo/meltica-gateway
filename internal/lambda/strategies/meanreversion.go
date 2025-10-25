package strategies

import (
	"context"
	"log"
	"sync"

	"github.com/coachpo/meltica/internal/schema"
)

// MeanReversion implements a mean reversion strategy that trades when price
// deviates significantly from its moving average.
type MeanReversion struct {
	Lambda interface {
		Logger() *log.Logger
		GetLastPrice() float64
		IsTradingActive() bool
		SubmitOrder(ctx context.Context, side schema.TradeSide, quantity string, price *float64) error
	}

	// Configuration
	WindowSize         int     // Moving average window size
	DeviationThreshold float64 // Deviation % to trigger trade
	OrderSize          string  // Order size as string

	// State
	mu          sync.Mutex
	prices      []float64
	movingAvg   float64
	hasPosition bool
}

var meanReversionSubscribedEvents = []schema.EventType{
	schema.EventTypeTrade,
	schema.EventTypeExecReport,
	schema.EventTypeBalanceUpdate,
}

// SubscribedEvents returns the list of event types this strategy subscribes to.
func (s *MeanReversion) SubscribedEvents() []schema.EventType {
	return append([]schema.EventType(nil), meanReversionSubscribedEvents...)
}

// OnTrade analyzes price deviation from moving average.
func (s *MeanReversion) OnTrade(ctx context.Context, _ *schema.Event, _ schema.TradePayload, price float64) {
	if !s.Lambda.IsTradingActive() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Update price history
	s.prices = append(s.prices, price)
	if len(s.prices) > s.WindowSize {
		s.prices = s.prices[len(s.prices)-s.WindowSize:]
	}

	// Need full window
	if len(s.prices) < s.WindowSize {
		return
	}

	// Calculate moving average
	sum := 0.0
	for _, p := range s.prices {
		sum += p
	}
	s.movingAvg = sum / float64(len(s.prices))

	// Calculate deviation
	deviation := (price - s.movingAvg) / s.movingAvg * 100

	s.Lambda.Logger().Printf("[MEAN_REV] Price: %.2f MA: %.2f Deviation: %.2f%%",
		price, s.movingAvg, deviation)

	// Price below MA by threshold - buy (expect reversion up)
	if deviation < -s.DeviationThreshold && !s.hasPosition {
		if err := s.Lambda.SubmitOrder(ctx, schema.TradeSideBuy, s.OrderSize, &price); err != nil {
			s.Lambda.Logger().Printf("[MEAN_REV] Failed to buy: %v", err)
		} else {
			s.Lambda.Logger().Printf("[MEAN_REV] BUY: price %.2f below MA %.2f (%.2f%%)",
				price, s.movingAvg, deviation)
			s.hasPosition = true
		}
	}

	// Price above MA by threshold - sell (expect reversion down)
	if deviation > s.DeviationThreshold && !s.hasPosition {
		if err := s.Lambda.SubmitOrder(ctx, schema.TradeSideSell, s.OrderSize, &price); err != nil {
			s.Lambda.Logger().Printf("[MEAN_REV] Failed to sell: %v", err)
		} else {
			s.Lambda.Logger().Printf("[MEAN_REV] SELL: price %.2f above MA %.2f (%.2f%%)",
				price, s.movingAvg, deviation)
			s.hasPosition = true
		}
	}

	// Close position when price reverts to MA
	if s.hasPosition && absFloat(deviation) < s.DeviationThreshold/2 {
		s.Lambda.Logger().Printf("[MEAN_REV] Price reverted to MA, position closed")
		s.hasPosition = false
	}
}

// OnTicker does nothing.
func (s *MeanReversion) OnTicker(_ context.Context, _ *schema.Event, _ schema.TickerPayload) {}

// OnBookSnapshot does nothing.
func (s *MeanReversion) OnBookSnapshot(_ context.Context, _ *schema.Event, _ schema.BookSnapshotPayload) {
}

// OnOrderFilled resets position flag.
func (s *MeanReversion) OnOrderFilled(_ context.Context, _ *schema.Event, payload schema.ExecReportPayload) {
	s.Lambda.Logger().Printf("[MEAN_REV] Order filled: side=%s price=%s",
		payload.Side, payload.AvgFillPrice)
}

// OnOrderRejected resets position flag.
func (s *MeanReversion) OnOrderRejected(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload, reason string) {
	s.mu.Lock()
	s.hasPosition = false
	s.mu.Unlock()
	s.Lambda.Logger().Printf("[MEAN_REV] Order rejected: %s", reason)
}

// OnOrderPartialFill logs partial fills.
func (s *MeanReversion) OnOrderPartialFill(_ context.Context, _ *schema.Event, payload schema.ExecReportPayload) {
	s.Lambda.Logger().Printf("[MEAN_REV] Partial fill: %s", payload.FilledQuantity)
}

// OnOrderCancelled resets position flag.
func (s *MeanReversion) OnOrderCancelled(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
	s.mu.Lock()
	s.hasPosition = false
	s.mu.Unlock()
	s.Lambda.Logger().Printf("[MEAN_REV] Order cancelled")
}

// OnOrderAcknowledged tracks acknowledged orders (no-op for this strategy).
func (s *MeanReversion) OnOrderAcknowledged(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
}

// OnOrderExpired tracks expired orders (no-op for this strategy).
func (s *MeanReversion) OnOrderExpired(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
}

// OnKlineSummary tracks kline data (no-op for this strategy).
func (s *MeanReversion) OnKlineSummary(_ context.Context, _ *schema.Event, _ schema.KlineSummaryPayload) {
}

// OnInstrumentUpdate is a no-op for this strategy.
func (s *MeanReversion) OnInstrumentUpdate(_ context.Context, _ *schema.Event, _ schema.InstrumentUpdatePayload) {
}

// OnBalanceUpdate logs balance updates for visibility.
func (s *MeanReversion) OnBalanceUpdate(_ context.Context, _ *schema.Event, payload schema.BalanceUpdatePayload) {
	s.Lambda.Logger().Printf("[MEAN_REV] Balance update: currency=%s total=%s available=%s",
		payload.Currency, payload.Total, payload.Available)
}
