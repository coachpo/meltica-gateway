package strategies

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/coachpo/meltica/internal/schema"
)

// Delay simulates processing latency without performing any business logic.
type Delay struct {
	MinDelay time.Duration
	MaxDelay time.Duration
}

const (
	// DefaultMinDelay defines the default lower bound for random delay.
	DefaultMinDelay = 100 * time.Millisecond
	// DefaultMaxDelay defines the default upper bound for random delay.
	DefaultMaxDelay = 500 * time.Millisecond
)

var (
	delaySubscribedEvents = []schema.RouteType{
		schema.RouteTypeTrade,
		schema.RouteTypeTicker,
		schema.RouteTypeOrderbookSnapshot,
		schema.RouteTypeAccountBalance,
	}
	delayRandMu sync.Mutex
)

// SubscribedEvents returns the list of event types this strategy subscribes to.
func (s *Delay) SubscribedEvents() []schema.RouteType {
	return append([]schema.RouteType(nil), delaySubscribedEvents...)
}

func (s *Delay) sleep() {
	minDelay := s.MinDelay
	maxDelay := s.MaxDelay
	if minDelay == 0 && maxDelay == 0 {
		minDelay = DefaultMinDelay
		maxDelay = DefaultMaxDelay
	}
	if minDelay < 0 {
		minDelay = 0
	}
	if maxDelay < 0 {
		maxDelay = 0
	}
	if maxDelay < minDelay {
		maxDelay = minDelay
	}

	delayRandMu.Lock()
	defer delayRandMu.Unlock()

	delayRange := maxDelay - minDelay
	if delayRange <= 0 {
		time.Sleep(minDelay)
		return
	}

	offset, err := randDuration(delayRange)
	if err != nil {
		// Fallback to midpoint delay if crypto/rand fails
		time.Sleep(minDelay + delayRange/2)
		return
	}
	time.Sleep(minDelay + offset)
}

func randDuration(maxDuration time.Duration) (time.Duration, error) {
	if maxDuration <= 0 {
		return 0, nil
	}
	nanos := maxDuration.Nanoseconds()
	if nanos <= 0 {
		return 0, nil
	}
	limit := big.NewInt(nanos + 1)
	value, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return 0, fmt.Errorf("crypto rand int: %w", err)
	}
	return time.Duration(value.Int64()), nil
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

// OnInstrumentUpdate handles instrument catalogue refreshes by adding a delay.
func (s *Delay) OnInstrumentUpdate(_ context.Context, _ *schema.Event, _ schema.InstrumentUpdatePayload) {
	s.sleep()
}

// OnBalanceUpdate handles balance updates by adding a delay.
func (s *Delay) OnBalanceUpdate(_ context.Context, _ *schema.Event, _ schema.BalanceUpdatePayload) {
	s.sleep()
}
