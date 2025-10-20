package strategies

import (
	"context"

	"github.com/coachpo/meltica/internal/schema"
)

// NoOp is a strategy that does nothing - useful for monitoring-only lambdas.
type NoOp struct{}

var noopSubscribedEvents = []schema.CanonicalType{
	schema.CanonicalType("TRADE"),
	schema.CanonicalType("TICKER"),
	schema.CanonicalType("ORDERBOOK.SNAPSHOT"),
}

// SubscribedEvents returns the list of event types this strategy subscribes to.
func (s *NoOp) SubscribedEvents() []schema.CanonicalType {
	return append([]schema.CanonicalType(nil), noopSubscribedEvents...)
}

// OnTrade does nothing.
func (s *NoOp) OnTrade(_ context.Context, _ *schema.Event, _ schema.TradePayload, _ float64) {}

// OnTicker does nothing.
func (s *NoOp) OnTicker(_ context.Context, _ *schema.Event, _ schema.TickerPayload) {}

// OnBookSnapshot does nothing.
func (s *NoOp) OnBookSnapshot(_ context.Context, _ *schema.Event, _ schema.BookSnapshotPayload) {}

// OnOrderFilled does nothing.
func (s *NoOp) OnOrderFilled(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {}

// OnOrderRejected does nothing.
func (s *NoOp) OnOrderRejected(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload, _ string) {
}

// OnOrderPartialFill does nothing.
func (s *NoOp) OnOrderPartialFill(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {}

// OnOrderCancelled does nothing.
func (s *NoOp) OnOrderCancelled(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {}

// OnOrderAcknowledged does nothing.
func (s *NoOp) OnOrderAcknowledged(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {}

// OnOrderExpired does nothing.
func (s *NoOp) OnOrderExpired(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {}

// OnKlineSummary does nothing.
func (s *NoOp) OnKlineSummary(_ context.Context, _ *schema.Event, _ schema.KlineSummaryPayload) {}

// OnControlAck does nothing.
func (s *NoOp) OnControlAck(_ context.Context, _ *schema.Event, _ schema.ControlAckPayload) {}

// OnControlResult does nothing.
func (s *NoOp) OnControlResult(_ context.Context, _ *schema.Event, _ schema.ControlResultPayload) {}
