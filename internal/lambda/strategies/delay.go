package strategies

import (
	"context"
	"crypto/rand"
	"math"
	"sync"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

// Delay simulates processing latency without performing any business logic.
type Delay struct{}

var (
	delaySubscribedEvents = []schema.CanonicalType{
		schema.CanonicalType("TRADE"),
		schema.CanonicalType("TICKER"),
		schema.CanonicalType("ORDERBOOK.SNAPSHOT"),
	}
	delayRandMu sync.Mutex
)

// SubscribedEvents returns the list of event types this strategy subscribes to.
func (s *Delay) SubscribedEvents() []schema.CanonicalType {
	return append([]schema.CanonicalType(nil), delaySubscribedEvents...)
}

func (s *Delay) sleep() {
	delayRandMu.Lock()
	defer delayRandMu.Unlock()
	
	// Generate cryptographically secure random delay between 100-500ms
	randBytes := make([]byte, 8)
	_, err := rand.Read(randBytes)
	if err != nil {
		// Fallback to fixed delay if crypto/rand fails
		time.Sleep(300 * time.Millisecond)
		return
	}
	
	// Convert bytes to int64
	randInt := int64(0)
	for i, b := range randBytes {
		randInt |= int64(b) << (i * 8)
	}
	
	// Use absolute value to avoid negative values and mod to get range
	delay := time.Duration((int64(math.Abs(float64(randInt))) % 401) + 100) * time.Millisecond // 100-500ms
	time.Sleep(delay)
}

// OnTrade handles trade events by adding a delay.
func (s *Delay) OnTrade(_ context.Context, _ *schema.Event, _ schema.TradePayload, _ float64) {
	s.sleep()
}

// OnTicker handles ticker events by adding a delay.
func (s *Delay) OnTicker(_ context.Context, _ *schema.Event, _ schema.TickerPayload) {
	s.sleep()
}

// OnBookSnapshot handles order book snapshot events by adding a delay.
func (s *Delay) OnBookSnapshot(_ context.Context, _ *schema.Event, _ schema.BookSnapshotPayload) {
	s.sleep()
}

// OnOrderFilled handles order fill events by adding a delay.
func (s *Delay) OnOrderFilled(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
	s.sleep()
}

// OnOrderRejected handles order rejection events by adding a delay.
func (s *Delay) OnOrderRejected(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload, _ string) {
	s.sleep()
}

// OnOrderPartialFill handles order partial fill events by adding a delay.
func (s *Delay) OnOrderPartialFill(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
	s.sleep()
}

// OnOrderCancelled handles order cancellation events by adding a delay.
func (s *Delay) OnOrderCancelled(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
	s.sleep()
}

// OnOrderAcknowledged handles order acknowledgment events by adding a delay.
func (s *Delay) OnOrderAcknowledged(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
	s.sleep()
}

// OnOrderExpired handles order expiration events by adding a delay.
func (s *Delay) OnOrderExpired(_ context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
	s.sleep()
}

// OnKlineSummary handles kline summary events by adding a delay.
func (s *Delay) OnKlineSummary(_ context.Context, _ *schema.Event, _ schema.KlineSummaryPayload) {
	s.sleep()
}

// OnControlAck handles control acknowledgment events by adding a delay.
func (s *Delay) OnControlAck(_ context.Context, _ *schema.Event, _ schema.ControlAckPayload) {
	s.sleep()
}

// OnControlResult handles control result events by adding a delay.
func (s *Delay) OnControlResult(_ context.Context, _ *schema.Event, _ schema.ControlResultPayload) {
	s.sleep()
}
