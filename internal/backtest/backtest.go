package backtest

import (
	"container/heap"
	"context"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/lambda"
	"github.com/coachpo/meltica/internal/schema"
)

type engineConfig struct {
	Clock        Clock
	LatencyModel LatencyModel
}

// EngineOption configures optional engine behaviour.
type EngineOption func(*engineConfig)

// WithClock overrides the default virtual clock used during the backtest.
func WithClock(clock Clock) EngineOption {
	return func(cfg *engineConfig) {
		cfg.Clock = clock
	}
}

// WithLatencyModel supplies a custom latency model for event replay.
func WithLatencyModel(model LatencyModel) EngineOption {
	return func(cfg *engineConfig) {
		cfg.LatencyModel = model
	}
}

// Engine orchestrates a backtest run by replaying market events and recording analytics.
type Engine struct {
	feeder   DataFeeder
	exchange SimulatedExchange
	strategy lambda.TradingStrategy
	clock    Clock
	latency  LatencyModel

	queue         eventQueue
	feederExhaust bool

	analytics   *Analytics
	analyticsMu sync.Mutex
}

// NewEngine creates a new backtest engine.
func NewEngine(feeder DataFeeder, exchange SimulatedExchange, strategy lambda.TradingStrategy, opts ...EngineOption) *Engine {
	cfg := engineConfig{
		Clock:        NewVirtualClock(time.Unix(0, 0)),
		LatencyModel: ConstantLatency{Value: 0},
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	eng := &Engine{
		feeder:    feeder,
		exchange:  exchange,
		strategy:  strategy,
		clock:     cfg.Clock,
		latency:   cfg.LatencyModel,
		queue:     eventQueue{},
		analytics: newAnalytics(),
	}
	heap.Init(&eng.queue)

	if setter, ok := exchange.(interface{ setObserver(FillObserver) }); ok {
		setter.setObserver(eng)
	}
	if clockSetter, ok := exchange.(interface{ setClock(Clock) }); ok {
		clockSetter.setClock(cfg.Clock)
	}

	return eng
}

// Run starts the backtest replay loop.
func (e *Engine) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		if !e.feederExhaust {
			evt, err := e.feeder.Next()
			if err != nil {
				if err == io.EOF {
					e.feederExhaust = true
				} else {
					return err
				}
			} else if evt != nil {
				e.pushEvent(evt)
			}
		}

		if e.queue.Len() == 0 {
			if e.feederExhaust {
				return nil
			}
			continue
		}

		next := e.queue.Peek()
		e.clock.AdvanceTo(next.timestamp)
		if delay := e.latency.Delay(next.event); delay > 0 {
			e.clock.Advance(delay)
		}

		item := heap.Pop(&e.queue).(*eventItem)
		e.dispatchEvent(ctx, item.event)
	}
}

// Analytics returns a snapshot of the current backtest analytics.
func (e *Engine) Analytics() Analytics {
	e.analyticsMu.Lock()
	defer e.analyticsMu.Unlock()
	return e.analytics.clone()
}

// OnOrderSubmitted is invoked by the simulated exchange when a new order is processed.
func (e *Engine) OnOrderSubmitted(_ schema.OrderRequest) {
	e.analyticsMu.Lock()
	e.analytics.recordOrder()
	e.analyticsMu.Unlock()
}

// OnFill updates analytics in response to simulated executions.
func (e *Engine) OnFill(symbol string, report schema.ExecReportPayload, fee decimal.Decimal) {
	quantity, errQty := decimal.NewFromString(report.FilledQuantity)
	price, errPrice := decimal.NewFromString(report.AvgFillPrice)
	if errQty != nil || errPrice != nil {
		return
	}

	e.analyticsMu.Lock()
	e.analytics.recordFill(symbol, report.Side, quantity, price, fee)
	e.analyticsMu.Unlock()
}

func (e *Engine) pushEvent(evt *schema.Event) {
	if evt == nil {
		return
	}
	item := &eventItem{event: evt, timestamp: deriveTimestamp(evt)}
	heap.Push(&e.queue, item)
}

func (e *Engine) dispatchEvent(ctx context.Context, evt *schema.Event) {
	switch payload := evt.Payload.(type) {
	case schema.TradePayload:
		e.updatePrice(evt.Symbol, payload.Price)
		price, _ := strconv.ParseFloat(payload.Price, 64)
		if e.strategy != nil {
			e.strategy.OnTrade(ctx, evt, payload, price)
		}
	case schema.TickerPayload:
		e.updatePrice(evt.Symbol, payload.LastPrice)
		if e.strategy != nil {
			e.strategy.OnTicker(ctx, evt, payload)
		}
	case schema.BookSnapshotPayload:
		e.updateBookMid(evt.Symbol, payload)
		if e.strategy != nil {
			e.strategy.OnBookSnapshot(ctx, evt, payload)
		}
	case schema.KlineSummaryPayload:
		e.updatePrice(evt.Symbol, payload.ClosePrice)
		if e.strategy != nil {
			e.strategy.OnKlineSummary(ctx, evt, payload)
		}
	case schema.BalanceUpdatePayload:
		if e.strategy != nil {
			e.strategy.OnBalanceUpdate(ctx, evt, payload)
		}
	case schema.ExecReportPayload:
		if e.strategy == nil {
			return
		}
		switch payload.State {
		case schema.ExecReportStateFILLED:
			e.strategy.OnOrderFilled(ctx, evt, payload)
		case schema.ExecReportStatePARTIAL:
			e.strategy.OnOrderPartialFill(ctx, evt, payload)
		case schema.ExecReportStateCANCELLED:
			e.strategy.OnOrderCancelled(ctx, evt, payload)
		case schema.ExecReportStateREJECTED:
			reason := ""
			if payload.RejectReason != nil {
				reason = *payload.RejectReason
			}
			e.strategy.OnOrderRejected(ctx, evt, payload, reason)
		case schema.ExecReportStateACK:
			e.strategy.OnOrderAcknowledged(ctx, evt, payload)
		case schema.ExecReportStateEXPIRED:
			e.strategy.OnOrderExpired(ctx, evt, payload)
		}
	case schema.InstrumentUpdatePayload:
		if e.strategy != nil {
			e.strategy.OnInstrumentUpdate(ctx, evt, payload)
		}
	}
}

func (e *Engine) updatePrice(symbol, priceStr string) {
	if priceStr == "" {
		return
	}
	price, err := decimal.NewFromString(priceStr)
	if err != nil {
		return
	}
	e.analyticsMu.Lock()
	e.analytics.updateMarketPrice(symbol, price)
	e.analyticsMu.Unlock()
}

func (e *Engine) updateBookMid(symbol string, payload schema.BookSnapshotPayload) {
	if len(payload.Bids) == 0 || len(payload.Asks) == 0 {
		return
	}
	bid, errBid := decimal.NewFromString(payload.Bids[0].Price)
	ask, errAsk := decimal.NewFromString(payload.Asks[0].Price)
	if errBid != nil || errAsk != nil {
		return
	}
	mid := bid.Add(ask).Div(decimal.NewFromInt(2))
	e.analyticsMu.Lock()
	e.analytics.updateMarketPrice(symbol, mid)
	e.analyticsMu.Unlock()
}

type eventItem struct {
	event     *schema.Event
	timestamp time.Time
	index     int
}

type eventQueue []*eventItem

func (eq eventQueue) Len() int { return len(eq) }

func (eq eventQueue) Less(i, j int) bool { return eq[i].timestamp.Before(eq[j].timestamp) }

func (eq eventQueue) Swap(i, j int) {
	eq[i], eq[j] = eq[j], eq[i]
	eq[i].index = i
	eq[j].index = j
}

func (eq *eventQueue) Push(x any) {
	item := x.(*eventItem)
	item.index = len(*eq)
	*eq = append(*eq, item)
}

func (eq *eventQueue) Pop() any {
	old := *eq
	n := len(old)
	item := old[n-1]
	item.index = -1
	*eq = old[:n-1]
	return item
}

func (eq eventQueue) Peek() *eventItem {
	if len(eq) == 0 {
		return nil
	}
	return eq[0]
}

func deriveTimestamp(evt *schema.Event) time.Time {
	if !evt.EmitTS.IsZero() {
		return evt.EmitTS
	}
	if !evt.IngestTS.IsZero() {
		return evt.IngestTS
	}
	switch payload := evt.Payload.(type) {
	case schema.TradePayload:
		if !payload.Timestamp.IsZero() {
			return payload.Timestamp
		}
	case schema.TickerPayload:
		if !payload.Timestamp.IsZero() {
			return payload.Timestamp
		}
	case schema.BookSnapshotPayload:
		if !payload.LastUpdate.IsZero() {
			return payload.LastUpdate
		}
	case schema.KlineSummaryPayload:
		if !payload.CloseTime.IsZero() {
			return payload.CloseTime
		}
	case schema.BalanceUpdatePayload:
		if !payload.Timestamp.IsZero() {
			return payload.Timestamp
		}
	case schema.ExecReportPayload:
		if !payload.Timestamp.IsZero() {
			return payload.Timestamp
		}
	}
	return time.Unix(0, 0)
}
