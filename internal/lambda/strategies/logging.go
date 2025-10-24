package strategies

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/coachpo/meltica/internal/schema"
)

// Logging logs all events - useful for debugging.
type Logging struct {
	Logger       *log.Logger
	LoggerPrefix string
}

var loggingSubscribedEvents = []schema.CanonicalType{
	schema.CanonicalType("TRADE"),
	schema.CanonicalType("TICKER"),
	schema.CanonicalType("ORDERBOOK.SNAPSHOT"),
	schema.CanonicalType("EXECUTION.REPORT"),
	schema.CanonicalTypeAccountBalance,
}

// SubscribedEvents returns the list of event types this strategy subscribes to.
func (s *Logging) SubscribedEvents() []schema.CanonicalType {
	return append([]schema.CanonicalType(nil), loggingSubscribedEvents...)
}

func (s *Logging) logger() *log.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	prefix := strings.TrimSpace(s.LoggerPrefix)
	if prefix == "" {
		prefix = "[Logging] "
	}
	s.Logger = log.New(os.Stdout, prefix, log.LstdFlags|log.Lmicroseconds)
	return s.Logger
}

// OnTrade logs trade events.
func (s *Logging) OnTrade(_ context.Context, evt *schema.Event, _ schema.TradePayload, price float64) {
	s.logger().Printf("Trade received: provider=%s symbol=%s price=%.2f", evt.Provider, evt.Symbol, price)
}

// OnTicker logs ticker events.
func (s *Logging) OnTicker(_ context.Context, evt *schema.Event, payload schema.TickerPayload) {
	s.logger().Printf("Ticker: provider=%s symbol=%s last=%s bid=%s ask=%s",
		evt.Provider, evt.Symbol, payload.LastPrice, payload.BidPrice, payload.AskPrice)
}

// OnBookSnapshot logs book snapshot events with truncated orderbook (top 5 levels).
func (s *Logging) OnBookSnapshot(_ context.Context, evt *schema.Event, payload schema.BookSnapshotPayload) {
	logger := s.logger()
	logger.Printf("Book snapshot: provider=%s symbol=%s %d bids, %d asks", evt.Provider, evt.Symbol, len(payload.Bids), len(payload.Asks))

	// Print top 5 bids
	bidLimit := 5
	if len(payload.Bids) < bidLimit {
		bidLimit = len(payload.Bids)
	}
	for i := 0; i < bidLimit; i++ {
		logger.Printf("  BID[%d]: %s @ %s", i, payload.Bids[i].Quantity, payload.Bids[i].Price)
	}

	// Print top 5 asks
	askLimit := 5
	if len(payload.Asks) < askLimit {
		askLimit = len(payload.Asks)
	}
	for i := 0; i < askLimit; i++ {
		logger.Printf("  ASK[%d]: %s @ %s", i, payload.Asks[i].Quantity, payload.Asks[i].Price)
	}
}

// OnOrderFilled logs filled order events.
func (s *Logging) OnOrderFilled(_ context.Context, evt *schema.Event, payload schema.ExecReportPayload) {
	s.logger().Printf("Order filled: provider=%s symbol=%s id=%s qty=%s price=%s",
		evt.Provider, evt.Symbol, payload.ClientOrderID, payload.FilledQuantity, payload.AvgFillPrice)
}

// OnOrderRejected logs rejected order events.
func (s *Logging) OnOrderRejected(_ context.Context, evt *schema.Event, payload schema.ExecReportPayload, reason string) {
	s.logger().Printf("Order rejected: provider=%s symbol=%s id=%s reason=%s", evt.Provider, evt.Symbol, payload.ClientOrderID, reason)
}

// OnOrderPartialFill logs partial fill events.
func (s *Logging) OnOrderPartialFill(_ context.Context, evt *schema.Event, payload schema.ExecReportPayload) {
	s.logger().Printf("Order partial fill: provider=%s symbol=%s id=%s filled=%s remaining=%s",
		evt.Provider, evt.Symbol, payload.ClientOrderID, payload.FilledQuantity, payload.RemainingQty)
}

// OnOrderCancelled logs cancelled order events.
func (s *Logging) OnOrderCancelled(_ context.Context, evt *schema.Event, payload schema.ExecReportPayload) {
	s.logger().Printf("Order cancelled: provider=%s symbol=%s id=%s", evt.Provider, evt.Symbol, payload.ClientOrderID)
}

// OnOrderAcknowledged logs order acknowledgment events.
func (s *Logging) OnOrderAcknowledged(_ context.Context, evt *schema.Event, payload schema.ExecReportPayload) {
	s.logger().Printf("Order acknowledged: provider=%s symbol=%s id=%s", evt.Provider, evt.Symbol, payload.ClientOrderID)
}

// OnOrderExpired logs expired order events.
func (s *Logging) OnOrderExpired(_ context.Context, evt *schema.Event, payload schema.ExecReportPayload) {
	s.logger().Printf("Order expired: provider=%s symbol=%s id=%s", evt.Provider, evt.Symbol, payload.ClientOrderID)
}

// OnKlineSummary logs kline summary events.
func (s *Logging) OnKlineSummary(_ context.Context, evt *schema.Event, payload schema.KlineSummaryPayload) {
	s.logger().Printf("Kline: provider=%s symbol=%s open=%s close=%s high=%s low=%s vol=%s",
		evt.Provider, evt.Symbol, payload.OpenPrice, payload.ClosePrice, payload.HighPrice, payload.LowPrice, payload.Volume)
}

// OnInstrumentUpdate logs instrument catalogue refresh events.
func (s *Logging) OnInstrumentUpdate(_ context.Context, evt *schema.Event, payload schema.InstrumentUpdatePayload) {
	s.logger().Printf("Instrument updated: provider=%s symbol=%s", evt.Provider, payload.Instrument.Symbol)
}

// OnBalanceUpdate logs account balance updates.
func (s *Logging) OnBalanceUpdate(_ context.Context, evt *schema.Event, payload schema.BalanceUpdatePayload) {
	s.logger().Printf("Balance update: provider=%s currency=%s total=%s available=%s",
		evt.Provider, payload.Currency, payload.Total, payload.Available)
}
