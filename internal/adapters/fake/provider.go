package fake

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coachpo/meltica/internal/adapters/shared"
	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

// Provider emits synthetic market data for tests and demos using a lightweight
// deterministic model.
type Provider struct {
	name  string
	opts  Options
	pools *pool.PoolManager
	clock func() time.Time

	events chan *schema.Event
	errs   chan error

	ctx     context.Context
	cancel  context.CancelFunc
	started atomic.Bool

	publisher *shared.Publisher

	instrumentsMu sync.RWMutex
	instruments   map[string]schema.Instrument
	states        map[string]*symbolMarketState

	orderSeq atomic.Uint64
}

// NewProvider creates a minimal fake provider instance.
func NewProvider(opts Options) *Provider {
	opts = withDefaults(opts)
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "fake"
	}

	events := make(chan *schema.Event, 128)
	errs := make(chan error, 8)

	p := &Provider{
		name:        name,
		opts:        opts,
		pools:       opts.Pools,
		clock:       time.Now,
		events:      events,
		errs:        errs,
		instruments: make(map[string]schema.Instrument),
		states:      make(map[string]*symbolMarketState),
	}
	if p.pools == nil {
		pm := pool.NewPoolManager()
		_ = pm.RegisterPool("Event", 256, func() any { return &schema.Event{} })
		p.pools = pm
	}
	p.publisher = shared.NewPublisher(p.name, p.events, p.pools, p.clock)
	p.seedCatalogue(opts.Instruments)
	return p
}

// Name returns the configured provider name.
func (p *Provider) Name() string { return p.name }

// Events exposes the canonical event stream.
func (p *Provider) Events() <-chan *schema.Event { return p.events }

// Errors exposes asynchronous provider errors.
func (p *Provider) Errors() <-chan error { return p.errs }

// Start activates the provider until the context is cancelled.
func (p *Provider) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("fake provider requires context")
	}
	if !p.started.CompareAndSwap(false, true) {
		return errors.New("fake provider already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	p.ctx = runCtx
	p.cancel = cancel

	go func() {
		<-runCtx.Done()
		close(p.events)
		close(p.errs)
	}()
	return nil
}

// SubmitOrder accepts orders without execution semantics to satisfy the provider interface.
func (p *Provider) SubmitOrder(_ context.Context, _ schema.OrderRequest) error {
	return nil
}

// SubscribeRoute is a no-op in the simplified provider.
func (p *Provider) SubscribeRoute(dispatcher.Route) error { return nil }

// UnsubscribeRoute is a no-op in the simplified provider.
func (p *Provider) UnsubscribeRoute(schema.RouteType) error { return nil }

// Instruments returns the current catalogue.
func (p *Provider) Instruments() []schema.Instrument {
	p.instrumentsMu.RLock()
	defer p.instrumentsMu.RUnlock()
	out := make([]schema.Instrument, 0, len(p.instruments))
	for _, inst := range p.instruments {
		out = append(out, schema.CloneInstrument(inst))
	}
	return out
}

// PublishTickerEvent emits a ticker for the requested instrument.
func (p *Provider) PublishTickerEvent(symbol string) error {
	if err := p.ensureRunning(); err != nil {
		return err
	}
	state, cons, err := p.instrumentStateFor(symbol)
	if err != nil {
		return err
	}
	ts := p.clock().UTC()
	price := p.nextPrice(state)
	bid := price * 0.999
	ask := price * 1.001
	payload := schema.TickerPayload{
		LastPrice: formatWithPrecision(price, cons.pricePrecision),
		BidPrice:  formatWithPrecision(bid, cons.pricePrecision),
		AskPrice:  formatWithPrecision(ask, cons.pricePrecision),
		Volume24h: formatWithPrecision(state.volume24h, cons.quantityPrecision),
		Timestamp: ts,
	}
	p.publisher.PublishTicker(p.ctx, state.symbol, payload)
	return nil
}

// PublishTradeEvent emits a deterministic synthetic trade.
func (p *Provider) PublishTradeEvent(symbol string) error {
	if err := p.ensureRunning(); err != nil {
		return err
	}
	state, cons, err := p.instrumentStateFor(symbol)
	if err != nil {
		return err
	}
	ts := p.clock().UTC()
	price := p.nextPrice(state)
	qty := cons.normalizeQuantity(1.0)
	state.volume24h += qty
	payload := schema.TradePayload{
		TradeID:   fmt.Sprintf("%s-%d", state.symbol, p.orderSeq.Add(1)),
		Side:      schema.TradeSideBuy,
		Price:     formatWithPrecision(price, cons.pricePrecision),
		Quantity:  formatWithPrecision(qty, cons.quantityPrecision),
		Timestamp: ts,
	}
	p.publisher.PublishTrade(p.ctx, state.symbol, payload)
	return nil
}

// PublishExecReport emits an execution report with sensible defaults.
func (p *Provider) PublishExecReport(symbol string, payload schema.ExecReportPayload) error {
	if err := p.ensureRunning(); err != nil {
		return err
	}
	state, cons, err := p.instrumentStateFor(symbol)
	if err != nil {
		return err
	}
	if payload.ExchangeOrderID == "" {
		payload.ExchangeOrderID = fmt.Sprintf("EX-%d", p.orderSeq.Add(1))
	}
	if payload.Price == "" {
		payload.Price = formatWithPrecision(state.lastPrice, cons.pricePrecision)
	}
	if payload.Quantity == "" {
		payload.Quantity = formatWithPrecision(1.0, cons.quantityPrecision)
	}
	if payload.Timestamp.IsZero() {
		payload.Timestamp = p.clock().UTC()
	}
	p.publisher.PublishExecReport(p.ctx, state.symbol, payload)
	return nil
}

func (p *Provider) ensureRunning() error {
	if !p.started.Load() || p.ctx == nil {
		return errors.New("fake provider not started")
	}
	return nil
}

func (p *Provider) instrumentStateFor(symbol string) (*symbolMarketState, instrumentConstraints, error) {
	normalized := normalizeInstrument(symbol)
	if normalized == "" {
		normalized = p.defaultInstrumentSymbol()
	}
	p.instrumentsMu.Lock()
	inst, ok := p.instruments[normalized]
	if !ok {
		p.instrumentsMu.Unlock()
		return nil, instrumentConstraints{}, fmt.Errorf("fake provider: instrument %s not found", symbol)
	}
	state, ok := p.states[normalized]
	if !ok {
		cons := constraintsFromInstrument(inst)
		state = newSymbolMarketState(inst.Symbol, defaultBasePrice(inst.Symbol), cons, p.opts.OrderBook.Levels)
		p.states[normalized] = state
	}
	p.instrumentsMu.Unlock()
	return state, state.constraints, nil
}

func (p *Provider) seedCatalogue(instruments []schema.Instrument) {
	catalog := instruments
	if len(catalog) == 0 {
		catalog = DefaultInstruments
	}
	p.instrumentsMu.Lock()
	defer p.instrumentsMu.Unlock()
	for _, inst := range catalog {
		normalized := normalizeInstrument(inst.Symbol)
		if normalized == "" {
			continue
		}
		cloned := schema.CloneInstrument(inst)
		p.instruments[normalized] = cloned
		if _, ok := p.states[normalized]; !ok {
			cons := constraintsFromInstrument(cloned)
			p.states[normalized] = newSymbolMarketState(cloned.Symbol, defaultBasePrice(cloned.Symbol), cons, p.opts.OrderBook.Levels)
		}
	}
}

func (p *Provider) defaultInstrumentSymbol() string {
	p.instrumentsMu.RLock()
	defer p.instrumentsMu.RUnlock()
	for symbol := range p.instruments {
		return symbol
	}
	return ""
}

func (p *Provider) nextPrice(state *symbolMarketState) float64 {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.lastPrice <= 0 {
		state.lastPrice = state.basePrice
	}
	delta := math.Max(state.lastPrice*0.001, 0.01)
	state.lastPrice += delta
	return state.lastPrice
}
