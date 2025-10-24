package strategies

import (
	"context"
	"log"
	"sync"

	"github.com/coachpo/meltica/internal/schema"
)

// Grid implements a grid trading strategy that places buy and sell orders
// at regular price intervals.
type Grid struct {
	Lambda interface {
		Logger() *log.Logger
		GetLastPrice() float64
		IsTradingActive() bool
		SubmitOrder(ctx context.Context, side schema.TradeSide, quantity string, price *float64) error
	}

	// Configuration
	GridLevels  int     // Number of grid levels above and below
	GridSpacing float64 // Spacing between levels as %
	OrderSize   string  // Order size per level
	BasePrice   float64 // Center price for the grid

	// State
	mu          sync.Mutex
	activeGrids map[float64]bool // Track active orders at each level
	initialized bool
}

var gridSubscribedEvents = []schema.CanonicalType{
	schema.CanonicalType("TRADE"),
	schema.CanonicalType("EXECUTION.REPORT"),
}

// SubscribedEvents returns the list of event types this strategy subscribes to.
func (s *Grid) SubscribedEvents() []schema.CanonicalType {
	return append([]schema.CanonicalType(nil), gridSubscribedEvents...)
}

// OnTrade initializes grid if needed.
func (s *Grid) OnTrade(ctx context.Context, _ *schema.Event, _ schema.TradePayload, price float64) {
	if !s.Lambda.IsTradingActive() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Initialize base price if not set
	if s.BasePrice == 0 {
		s.BasePrice = price
		s.Lambda.Logger().Printf("[GRID] Base price set to %.2f", s.BasePrice)
	}

	// Initialize grid orders
	if !s.initialized {
		s.activeGrids = make(map[float64]bool)
		s.placeGridOrders(ctx)
		s.initialized = true
	}
}

// OnTicker does nothing.
func (s *Grid) OnTicker(_ context.Context, _ *schema.Event, _ schema.TickerPayload) {}

// OnBookSnapshot does nothing.
func (s *Grid) OnBookSnapshot(_ context.Context, _ *schema.Event, _ schema.BookSnapshotPayload) {}

// OnOrderFilled replaces the filled order with a new one on the opposite side.
func (s *Grid) OnOrderFilled(ctx context.Context, _ *schema.Event, payload schema.ExecReportPayload) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fillPrice, err := ParseFloat(payload.AvgFillPrice)
	if err != nil {
		s.Lambda.Logger().Printf("[GRID] Failed to parse fill price: %v", err)
		return
	}

	// Remove from active grids
	delete(s.activeGrids, fillPrice)

	// Place opposite order
	oppositeSide := schema.TradeSideSell
	if payload.Side == schema.TradeSideSell {
		oppositeSide = schema.TradeSideBuy
	}

	if err := s.Lambda.SubmitOrder(ctx, oppositeSide, s.OrderSize, &fillPrice); err != nil {
		s.Lambda.Logger().Printf("[GRID] Failed to place opposite order: %v", err)
	} else {
		s.activeGrids[fillPrice] = true
		s.Lambda.Logger().Printf("[GRID] Filled %s at %.2f, placed %s order",
			payload.Side, fillPrice, oppositeSide)
	}
}

// OnOrderRejected removes from active grids.
func (s *Grid) OnOrderRejected(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload, reason string) {
	s.Lambda.Logger().Printf("[GRID] Order rejected: %s", reason)
}

// OnOrderPartialFill logs partial fills.
func (s *Grid) OnOrderPartialFill(_ context.Context, _ *schema.Event, payload schema.ExecReportPayload) {
	s.Lambda.Logger().Printf("[GRID] Partial fill: %s", payload.FilledQuantity)
}

// OnOrderCancelled removes from active grids.
func (s *Grid) OnOrderCancelled(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
	s.Lambda.Logger().Printf("[GRID] Order cancelled")
}

func (s *Grid) placeGridOrders(ctx context.Context) {
	spacingMultiplier := s.GridSpacing / 100.0

	// Place buy orders below base price
	for i := 1; i <= s.GridLevels; i++ {
		buyPrice := s.BasePrice * (1 - spacingMultiplier*float64(i))
		if err := s.Lambda.SubmitOrder(ctx, schema.TradeSideBuy, s.OrderSize, &buyPrice); err != nil {
			s.Lambda.Logger().Printf("[GRID] Failed to place buy order at %.2f: %v", buyPrice, err)
		} else {
			s.activeGrids[buyPrice] = true
			s.Lambda.Logger().Printf("[GRID] Placed buy order at %.2f", buyPrice)
		}
	}

	// Place sell orders above base price
	for i := 1; i <= s.GridLevels; i++ {
		sellPrice := s.BasePrice * (1 + spacingMultiplier*float64(i))
		if err := s.Lambda.SubmitOrder(ctx, schema.TradeSideSell, s.OrderSize, &sellPrice); err != nil {
			s.Lambda.Logger().Printf("[GRID] Failed to place sell order at %.2f: %v", sellPrice, err)
		} else {
			s.activeGrids[sellPrice] = true
			s.Lambda.Logger().Printf("[GRID] Placed sell order at %.2f", sellPrice)
		}
	}
}

// OnOrderAcknowledged tracks acknowledged orders (no-op for this strategy).
func (s *Grid) OnOrderAcknowledged(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {}

// OnOrderExpired tracks expired orders (no-op for this strategy).
func (s *Grid) OnOrderExpired(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {}

// OnKlineSummary tracks kline data (no-op for this strategy).
func (s *Grid) OnKlineSummary(_ context.Context, _ *schema.Event, _ schema.KlineSummaryPayload) {}

// OnControlAck tracks control acknowledgments (no-op for this strategy).
func (s *Grid) OnControlAck(_ context.Context, _ *schema.Event, _ schema.ControlAckPayload) {}

// OnControlResult tracks control results (no-op for this strategy).
func (s *Grid) OnControlResult(_ context.Context, _ *schema.Event, _ schema.ControlResultPayload) {}

func (s *Grid) OnInstrumentUpdate(_ context.Context, _ *schema.Event, _ schema.InstrumentUpdatePayload) {
}
