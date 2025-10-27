package shared

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

// Publisher is a helper for creating and emitting canonical events.
type Publisher struct {
	providerName string
	events       chan<- *schema.Event
	pools        *pool.PoolManager
	clock        func() time.Time
	seqMu        sync.Mutex
	seq          map[string]uint64
}

// NewPublisher creates a new shared event publisher.
func NewPublisher(providerName string, events chan<- *schema.Event, pools *pool.PoolManager, clock func() time.Time) *Publisher {
	if clock == nil {
		clock = time.Now
	}
	return &Publisher{
		providerName: providerName,
		events:       events,
		pools:        pools,
		clock:        clock,
		seq:          make(map[string]uint64),
	}
}

// PublishTicker creates and emits a ticker event.
func (p *Publisher) PublishTicker(ctx context.Context, symbol string, payload schema.TickerPayload) {
	seq := p.nextSeq(schema.EventTypeTicker, symbol)
	evt := p.newEvent(ctx, schema.EventTypeTicker, symbol, seq, payload, payload.Timestamp)
	if evt == nil {
		return
	}
	p.emitEvent(ctx, evt)
}

// PublishTrade creates and emits a trade event.
func (p *Publisher) PublishTrade(ctx context.Context, symbol string, payload schema.TradePayload) {
	seq := p.nextSeq(schema.EventTypeTrade, symbol)
	evt := p.newEvent(ctx, schema.EventTypeTrade, symbol, seq, payload, payload.Timestamp)
	if evt == nil {
		return
	}
	p.emitEvent(ctx, evt)
}

// PublishBookSnapshot creates and emits an order book snapshot event.
func (p *Publisher) PublishBookSnapshot(ctx context.Context, symbol string, payload schema.BookSnapshotPayload) {
	seq := p.nextSeq(schema.EventTypeBookSnapshot, symbol)
	evt := p.newEvent(ctx, schema.EventTypeBookSnapshot, symbol, seq, payload, payload.LastUpdate)
	if evt == nil {
		return
	}
	p.emitEvent(ctx, evt)
}

// PublishBalanceUpdate creates and emits a balance update event.
func (p *Publisher) PublishBalanceUpdate(ctx context.Context, currency string, payload schema.BalanceUpdatePayload) {
	seq := p.nextSeq(schema.EventTypeBalanceUpdate, currency)
	evt := p.newEvent(ctx, schema.EventTypeBalanceUpdate, currency, seq, payload, payload.Timestamp)
	if evt == nil {
		return
	}
	p.emitEvent(ctx, evt)
}

// PublishExecReport creates and emits an execution report event.
func (p *Publisher) PublishExecReport(ctx context.Context, symbol string, payload schema.ExecReportPayload) {
	seq := p.nextSeq(schema.EventTypeExecReport, symbol)
	evt := p.newEvent(ctx, schema.EventTypeExecReport, symbol, seq, payload, payload.Timestamp)
	if evt == nil {
		return
	}
	p.emitEvent(ctx, evt)
}

// PublishKlineSummary creates and emits a kline summary event.
func (p *Publisher) PublishKlineSummary(ctx context.Context, symbol string, payload schema.KlineSummaryPayload) {
	seq := p.nextSeq(schema.EventTypeKlineSummary, symbol)
	evt := p.newEvent(ctx, schema.EventTypeKlineSummary, symbol, seq, payload, payload.CloseTime)
	if evt == nil {
		return
	}
	p.emitEvent(ctx, evt)
}

// PublishInstrumentUpdate creates and emits an instrument update event.
func (p *Publisher) PublishInstrumentUpdate(ctx context.Context, symbol string, payload schema.InstrumentUpdatePayload) {
	seq := p.nextSeq(schema.EventTypeInstrumentUpdate, symbol)
	evt := p.newEvent(ctx, schema.EventTypeInstrumentUpdate, symbol, seq, payload, p.clock().UTC())
	if evt == nil {
		return
	}
	p.emitEvent(ctx, evt)
}

func (p *Publisher) newEvent(ctx context.Context, evtType schema.EventType, symbol string, seq uint64, payload any, ts time.Time) *schema.Event {
	if ts.IsZero() {
		ts = p.clock().UTC()
	}
	eventID := fmt.Sprintf("%s:%s:%s:%d", p.providerName, strings.ReplaceAll(symbol, "-", ""), evtType, seq)
	evt, err := p.pools.BorrowEventInst(ctx)
	if err != nil {
		log.Printf("publisher for %s: borrow canonical event failed: %v", p.providerName, err)
		return nil
	}
	evt.EventID = eventID
	evt.Provider = p.providerName
	evt.Symbol = symbol
	evt.Type = evtType
	evt.SeqProvider = seq
	evt.IngestTS = ts
	evt.EmitTS = ts
	evt.Payload = payload
	return evt
}

func (p *Publisher) emitEvent(ctx context.Context, evt *schema.Event) {
	if evt == nil {
		return
	}
	select {
	case <-ctx.Done():
		p.pools.ReturnEvent(evt)
		return
	case p.events <- evt:
	}
}

func (p *Publisher) nextSeq(evtType schema.EventType, symbol string) uint64 {
	key := fmt.Sprintf("%s|%s", evtType, symbol)
	p.seqMu.Lock()
	defer p.seqMu.Unlock()
	p.seq[key]++
	return p.seq[key]
}