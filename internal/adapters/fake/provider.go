// Package fake provides a synthetic market data provider for testing and development.
package fake

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
	"github.com/sourcegraph/conc"
	"github.com/sourcegraph/conc/iter"
)

// DefaultInstruments lists canonical instruments used when no explicit filters are provided.
var DefaultInstruments = []string{
	"BTC-USDT",
	"ETH-USDT",
	"XRP-USDT",
	"SOL-USDT",
	"ADA-USDT",
	"DOGE-USDT",
	"BNB-USDT",
	"LTC-USDT",
	"DOT-USDT",
	"AVAX-USDT",
}

// Options configures the fake provider runtime.
type Options struct {
	Name                 string
	TickerInterval       time.Duration
	TradeInterval        time.Duration
	BookSnapshotInterval time.Duration
	Pools                *pool.PoolManager
}

// Provider emits synthetic market data covering tickers, trades, and order book events.
type Provider struct {
	name                 string
	tickerInterval       time.Duration
	tradeInterval        time.Duration
	bookSnapshotInterval time.Duration

	events chan *schema.Event
	errs   chan error
	orders chan schema.OrderRequest

	ctx    context.Context
	cancel context.CancelFunc

	started atomic.Bool

	mu     sync.Mutex
	routes map[schema.CanonicalType]*routeHandle

	pools *pool.PoolManager

	seqMu sync.Mutex
	seq   map[string]uint64

	stateMu sync.Mutex
	state   map[string]*instrumentState

	clock func() time.Time
}

type routeHandle struct {
	cancel context.CancelFunc
	wg     conc.WaitGroup
}

type instrumentState struct {
	mu          sync.Mutex
	instrument  string
	basePrice   float64
	lastPrice   float64
	volume      float64
	bids        []bookLevel
	asks        []bookLevel
	hasSnapshot bool
}

type bookLevel struct {
	price    float64
	quantity float64
}

// NewProvider constructs a fake data provider with sane defaults.
func NewProvider(opts Options) *Provider {
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "fake"
	}
	tickerInterval := opts.TickerInterval
	if tickerInterval <= 0 {
		tickerInterval = time.Second
	}
	tradeInterval := opts.TradeInterval
	if tradeInterval <= 0 {
		tradeInterval = 500 * time.Millisecond
	}
	bookSnapshotInterval := opts.BookSnapshotInterval
	if bookSnapshotInterval <= 0 {
		bookSnapshotInterval = 5 * time.Second
	}

	//nolint:exhaustruct // zero values for ctx, cancel, started, mu, etc. are intentional
	p := &Provider{
		name:                 name,
		tickerInterval:       tickerInterval,
		tradeInterval:        tradeInterval,
		bookSnapshotInterval: bookSnapshotInterval,
		events:               make(chan *schema.Event, 128),
		errs:                 make(chan error, 8),
		orders:               make(chan schema.OrderRequest, 64),
		routes:               make(map[schema.CanonicalType]*routeHandle),
		seq:                  make(map[string]uint64),
		state:                make(map[string]*instrumentState),
		clock:                time.Now,
		pools:                opts.Pools,
	}
	return p
}

// Start begins emitting synthetic events until the context is cancelled.
func (p *Provider) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("provider requires context")
	}
	if !p.started.CompareAndSwap(false, true) {
		return errors.New("provider already started")
	}

	ctx, cancel := context.WithCancel(ctx)
	p.ctx = ctx
	p.cancel = cancel

	go func() {
		<-ctx.Done()
		p.stopAll()
		close(p.orders)
		close(p.events)
		close(p.errs)
	}()

	go p.consumeOrders(ctx)
	return nil
}

func (p *Provider) Name() string {
	return p.name
}

// Events returns the canonical event stream.
func (p *Provider) Events() <-chan *schema.Event {
	return p.events
}

// Errors returns asynchronous provider errors.
func (p *Provider) Errors() <-chan error {
	return p.errs
}

// SubmitOrder enqueues a synthetic order acknowledgement.
func (p *Provider) SubmitOrder(ctx context.Context, req schema.OrderRequest) error {
	if !p.started.Load() {
		return errors.New("provider not started")
	}
	if req.Provider == "" {
		req.Provider = p.name
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("submit order context: %w", ctx.Err())
	case <-p.ctx.Done():
		return errors.New("provider shutting down")
	case p.orders <- req:
		return nil
	}
}

// SubscribeRoute activates a dispatcher route.
func (p *Provider) SubscribeRoute(route dispatcher.Route) error {
	if route.Type == "" {
		return errors.New("route type required")
	}
	if p.ctx == nil {
		return errors.New("provider not started")
	}
	evtType, ok := canonicalToEventType(route.Type)
	if !ok {
		return fmt.Errorf("unsupported canonical type %s", route.Type)
	}
	key := route.Type
	p.mu.Lock()
	if _, exists := p.routes[key]; exists {
		p.mu.Unlock()
		return nil
	}
	handle := p.startRouteLocked(route, evtType)
	p.routes[key] = handle
	p.mu.Unlock()
	return nil
}

// UnsubscribeRoute stops streaming for the canonical type.
func (p *Provider) UnsubscribeRoute(typ schema.CanonicalType) error {
	if typ == "" {
		return errors.New("route type required")
	}
	p.mu.Lock()
	handle, ok := p.routes[typ]
	if ok {
		delete(p.routes, typ)
	}
	p.mu.Unlock()
	if ok && handle != nil {
		handle.cancel()
		handle.wg.Wait()
	}
	return nil
}

func (p *Provider) startRouteLocked(route dispatcher.Route, evtType schema.EventType) *routeHandle {
	routeCtx, cancel := context.WithCancel(p.ctx)
	//nolint:exhaustruct // zero value for wg is intentional
	handle := &routeHandle{cancel: cancel}
	instruments := instrumentsFromRoute(route)
	if len(instruments) == 0 {
		instruments = normaliseInstruments(DefaultInstruments)
	}
	handle.wg.Go(func() {
		p.runGenerator(routeCtx, evtType, instruments)
	})
	return handle
}

func (p *Provider) runGenerator(ctx context.Context, evtType schema.EventType, instruments []string) {
	switch evtType {
	case schema.EventTypeTicker:
		p.streamTickers(ctx, instruments)
	case schema.EventTypeTrade:
		p.streamTrades(ctx, instruments)
	case schema.EventTypeBookSnapshot:
		p.streamBookSnapshots(ctx, instruments)
	case schema.EventTypeExecReport, schema.EventTypeKlineSummary, schema.EventTypeControlAck, schema.EventTypeControlResult:
		<-ctx.Done()
	default:
		<-ctx.Done()
	}
}

func (p *Provider) streamTickers(ctx context.Context, instruments []string) {
	ticker := time.NewTicker(p.tickerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, inst := range instruments {
				p.emitTicker(inst)
			}
		}
	}
}

func (p *Provider) streamTrades(ctx context.Context, instruments []string) {
	ticker := time.NewTicker(p.tradeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, inst := range instruments {
				p.emitTrade(inst)
			}
		}
	}
}

func (p *Provider) streamBookSnapshots(ctx context.Context, instruments []string) {
	p.emitSnapshots(instruments)
	ticker := time.NewTicker(p.bookSnapshotInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.emitSnapshots(instruments)
		}
	}
}

func (p *Provider) emitSnapshots(instruments []string) {
	for _, inst := range instruments {
		p.emitBookSnapshot(inst)
	}
}

func (p *Provider) emitTicker(instrument string) {
	state := p.getInstrumentState(instrument)
	ts := p.clock().UTC()
	seq := p.nextSeq(schema.EventTypeTicker, instrument)
	state.mu.Lock()
	price := p.nextPrice(state, seq)
	state.lastPrice = price
	state.volume += 25 + float64(seq%50)
	payload := schema.TickerPayload{
		LastPrice: formatPrice(price),
		BidPrice:  formatPrice(price * 0.9995),
		AskPrice:  formatPrice(price * 1.0005),
		Volume24h: formatQuantity(state.volume),
		Timestamp: ts,
	}
	state.mu.Unlock()
	evt := p.newEvent(schema.EventTypeTicker, instrument, seq, payload, ts)
	if evt == nil {
		return
	}
	p.emitEvent(evt)
}

func (p *Provider) emitTrade(instrument string) {
	state := p.getInstrumentState(instrument)
	ts := p.clock().UTC()
	seq := p.nextSeq(schema.EventTypeTrade, instrument)
	state.mu.Lock()
	price := p.nextPrice(state, seq)
	state.lastPrice = price
	quantity := 0.25 + 0.05*float64((seq%7)+1)
	state.volume += quantity
	side := schema.TradeSideBuy
	if seq%2 == 0 {
		side = schema.TradeSideSell
	}
	tradeID := fmt.Sprintf("%s-%d", strings.ReplaceAll(instrument, "-", ""), seq)
	payload := schema.TradePayload{
		TradeID:   tradeID,
		Side:      side,
		Price:     formatPrice(price),
		Quantity:  formatQuantity(quantity),
		Timestamp: ts,
	}
	state.mu.Unlock()
	evt := p.newEvent(schema.EventTypeTrade, instrument, seq, payload, ts)
	if evt == nil {
		return
	}
	p.emitEvent(evt)
}

func (p *Provider) emitBookSnapshot(instrument string) {
	state := p.getInstrumentState(instrument)
	ts := p.clock().UTC()
	seq := p.nextSeq(schema.EventTypeBookSnapshot, instrument)
	state.mu.Lock()
	state.reseedOrderBook()
	levelsBids := toPriceLevels(state.bids)
	levelsAsks := toPriceLevels(state.asks)
	state.hasSnapshot = true
	checksum := checksum(instrument, schema.EventTypeBookSnapshot, seq)
	state.mu.Unlock()
	//nolint:exhaustruct // FirstUpdateID and FinalUpdateID not used by fake provider
	payload := schema.BookSnapshotPayload{
		Bids:       levelsBids,
		Asks:       levelsAsks,
		Checksum:   checksum,
		LastUpdate: ts,
	}
	evt := p.newEvent(schema.EventTypeBookSnapshot, instrument, seq, payload, ts)
	if evt == nil {
		return
	}
	p.emitEvent(evt)
}

func (p *Provider) newEvent(evtType schema.EventType, instrument string, seq uint64, payload any, ts time.Time) *schema.Event {
	if ts.IsZero() {
		ts = p.clock().UTC()
	}
	symbol := normalizeInstrument(instrument)
	eventID := fmt.Sprintf("%s:%s:%s:%d", p.name, strings.ReplaceAll(symbol, "-", ""), evtType, seq)
	evt := p.borrowEvent(p.ctx)
	if evt == nil {
		return nil
	}
	evt.EventID = eventID
	evt.Provider = p.name
	evt.Symbol = symbol
	evt.Type = evtType
	evt.SeqProvider = seq
	evt.IngestTS = ts
	evt.EmitTS = ts
	evt.Payload = payload
	return evt
}

func (p *Provider) borrowEvent(ctx context.Context) *schema.Event {
	requestCtx := ctx
	if requestCtx == nil {
		requestCtx = p.ctx
	}
	if requestCtx == nil {
		requestCtx = context.Background()
	}
	evt, err := p.pools.BorrowEventInst(requestCtx)
	if err != nil {
		log.Printf("fake provider %s: borrow canonical event failed: %v", p.name, err)
		p.emitError(fmt.Errorf("borrow canonical event: %w", err))
		return nil
	}
	return evt
}

func (p *Provider) emitEvent(evt *schema.Event) {
	if evt == nil {
		return
	}
	if p.ctx == nil {
		return
	}
	select {
	case <-p.ctx.Done():
		return
	case p.events <- evt:
	}
}

func (p *Provider) emitError(err error) {
	if err == nil {
		return
	}
	if p.ctx == nil {
		return
	}
	select {
	case <-p.ctx.Done():
		return
	case p.errs <- err:
	default:
	}
}

func (p *Provider) consumeOrders(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case order, ok := <-p.orders:
			if !ok {
				return
			}
			p.handleOrder(order)
		}
	}
}

func (p *Provider) handleOrder(order schema.OrderRequest) {
	if strings.TrimSpace(order.Symbol) == "" {
		order.Symbol = DefaultInstruments[0]
	}
	if order.Timestamp.IsZero() {
		order.Timestamp = p.clock().UTC()
	}

	seq := p.nextSeq(schema.EventTypeExecReport, order.Symbol)
	ts := p.clock().UTC()
	//nolint:exhaustruct // zero value for RejectReason is intentional
	payload := schema.ExecReportPayload{
		ClientOrderID:   order.ClientOrderID,
		ExchangeOrderID: order.ClientOrderID,
		State:           schema.ExecReportStateACK,
		Side:            order.Side,
		OrderType:       order.OrderType,
		Price:           stringOrDefault(order.Price),
		Quantity:        order.Quantity,
		FilledQuantity:  "0",
		RemainingQty:    order.Quantity,
		AvgFillPrice:    stringOrDefault(order.Price),
		Timestamp:       ts,
	}
	evt := p.newEvent(schema.EventTypeExecReport, order.Symbol, seq, payload, ts)
	if evt != nil {
		p.emitEvent(evt)
	}
}

func (p *Provider) stopAll() {
	p.mu.Lock()
	handles := make([]*routeHandle, 0, len(p.routes))
	for typ, handle := range p.routes {
		handles = append(handles, handle)
		delete(p.routes, typ)
	}
	p.mu.Unlock()
	for _, handle := range handles {
		if handle == nil {
			continue
		}
		handle.cancel()
		handle.wg.Wait()
	}
}

func (p *Provider) nextSeq(evtType schema.EventType, instrument string) uint64 {
	key := fmt.Sprintf("%s|%s", evtType, normalizeInstrument(instrument))
	p.seqMu.Lock()
	seq := p.seq[key] + 1
	p.seq[key] = seq
	p.seqMu.Unlock()
	return seq
}

func (p *Provider) getInstrumentState(instrument string) *instrumentState {
	symbol := normalizeInstrument(instrument)
	p.stateMu.Lock()
	state, ok := p.state[symbol]
	if !ok {
		state = newInstrumentState(symbol, defaultBasePrice(symbol))
		p.state[symbol] = state
	}
	p.stateMu.Unlock()
	return state
}

func newInstrumentState(symbol string, basePrice float64) *instrumentState {
	//nolint:exhaustruct // zero values for mu, bids, asks, hasSnapshot are intentional
	state := &instrumentState{
		instrument: symbol,
		basePrice:  basePrice,
		lastPrice:  basePrice,
		volume:     1000,
	}
	state.reseedOrderBook()
	return state
}

func (s *instrumentState) reseedOrderBook() {
	levels := 5
	s.bids = make([]bookLevel, levels)
	s.asks = make([]bookLevel, levels)
	for i := 0; i < levels; i++ {
		delta := float64(i+1) * 0.5
		s.bids[i] = bookLevel{price: s.lastPrice - delta, quantity: 1.5 + 0.1*float64(i)}
		s.asks[i] = bookLevel{price: s.lastPrice + delta, quantity: 1.2 + 0.1*float64(i)}
	}
}

func normaliseInstruments(instruments []string) []string {
	set := make(map[string]struct{})
	for _, inst := range instruments {
		symbol := normalizeInstrument(inst)
		if symbol == "" {
			continue
		}
		set[symbol] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for inst := range set {
		out = append(out, inst)
	}
	sort.Strings(out)
	return out
}

func normalizeInstrument(symbol string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	return symbol
}

func defaultBasePrice(symbol string) float64 {
	switch symbol {
	case "BTC-USDT":
		return 60000
	case "ETH-USDT":
		return 2000
	case "SOL-USDT":
		return 150
	default:
		return 100
	}
}

func (p *Provider) nextPrice(state *instrumentState, seq uint64) float64 {
	base := state.lastPrice
	amplitude := 0.75 * math.Sin(float64(seq%13))
	price := base + amplitude
	if price <= 0 {
		price = state.basePrice
	}
	return price
}

func toPriceLevels(levels []bookLevel) []schema.PriceLevel {
	return iter.Map(levels, func(level *bookLevel) schema.PriceLevel {
		return schema.PriceLevel{Price: formatPrice(level.price), Quantity: formatQuantity(level.quantity)}
	})
}

func instrumentsFromRoute(route dispatcher.Route) []string {
	set := make(map[string]struct{})
	for _, filter := range route.Filters {
		if strings.EqualFold(filter.Field, "instrument") {
			collectInstrumentValues(filter.Value, set)
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for inst := range set {
		out = append(out, inst)
	}
	sort.Strings(out)
	return out
}

func collectInstrumentValues(value any, set map[string]struct{}) {
	switch v := value.(type) {
	case string:
		symbol := normalizeInstrument(v)
		if symbol != "" {
			set[symbol] = struct{}{}
		}
	case []string:
		for _, entry := range v {
			collectInstrumentValues(entry, set)
		}
	case []any:
		for _, entry := range v {
			collectInstrumentValues(entry, set)
		}
	}
}

func canonicalToEventType(c schema.CanonicalType) (schema.EventType, bool) {
	switch c {
	case schema.CanonicalType("ORDERBOOK.SNAPSHOT"), schema.CanonicalType("ORDERBOOK.DELTA"), schema.CanonicalType("ORDERBOOK.UPDATE"):
		return schema.EventTypeBookSnapshot, true
	case schema.CanonicalType("TRADE"):
		return schema.EventTypeTrade, true
	case schema.CanonicalType("TICKER"):
		return schema.EventTypeTicker, true
	case schema.CanonicalType("EXECUTION.REPORT"):
		return schema.EventTypeExecReport, true
	default:
		return "", false
	}
}

func checksum(instrument string, evtType schema.EventType, seq uint64) string {
	base := fmt.Sprintf("%s|%s|%d", instrument, evtType, seq)
	sum := uint32(0)
	for _, r := range base {
		sum = sum*33 + uint32(r)
	}
	return fmt.Sprintf("%08d", sum%100000000)
}

func formatPrice(value float64) string {
	return fmt.Sprintf("%.2f", value)
}

func formatQuantity(value float64) string {
	return fmt.Sprintf("%.4f", value)
}

func stringOrDefault(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
