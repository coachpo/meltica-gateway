package strategies

import (
	"context"
	"math/rand"
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
	delayRand   = rand.New(rand.NewSource(time.Now().UnixNano()))
)

func (s *Delay) SubscribedEvents() []schema.CanonicalType {
	return append([]schema.CanonicalType(nil), delaySubscribedEvents...)
}

func (s *Delay) sleep() {
	delayRandMu.Lock()
	defer delayRandMu.Unlock()
	d := time.Duration(delayRand.Intn(401)+100) * time.Millisecond // 100-500ms
	time.Sleep(d)
}

func (s *Delay) OnTrade(ctx context.Context, _ *schema.Event, _ schema.TradePayload, _ float64) {
	s.sleep()
}

func (s *Delay) OnTicker(ctx context.Context, _ *schema.Event, _ schema.TickerPayload) {
	s.sleep()
}

func (s *Delay) OnBookSnapshot(ctx context.Context, _ *schema.Event, _ schema.BookSnapshotPayload) {
	s.sleep()
}

func (s *Delay) OnOrderFilled(ctx context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
	s.sleep()
}

func (s *Delay) OnOrderRejected(ctx context.Context, _ *schema.Event, _ schema.ExecReportPayload, _ string) {
	s.sleep()
}

func (s *Delay) OnOrderPartialFill(ctx context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
	s.sleep()
}

func (s *Delay) OnOrderCancelled(ctx context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
	s.sleep()
}

func (s *Delay) OnOrderAcknowledged(ctx context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
	s.sleep()
}

func (s *Delay) OnOrderExpired(ctx context.Context, _ *schema.Event, _ schema.ExecReportPayload) {
	s.sleep()
}

func (s *Delay) OnKlineSummary(ctx context.Context, _ *schema.Event, _ schema.KlineSummaryPayload) {
	s.sleep()
}

func (s *Delay) OnControlAck(ctx context.Context, _ *schema.Event, _ schema.ControlAckPayload) {
	s.sleep()
}

func (s *Delay) OnControlResult(ctx context.Context, _ *schema.Event, _ schema.ControlResultPayload) {
	s.sleep()
}
