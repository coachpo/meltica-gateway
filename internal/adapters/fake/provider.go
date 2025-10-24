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

// DefaultInstruments lists canonical instruments used when no explicit catalogue is provided.
var DefaultInstruments = []schema.Instrument{
	newSpotInstrument("BTC-USDT", "BTC", "USDT"),
	newSpotInstrument("ETH-USDT", "ETH", "USDT"),
	newSpotInstrument("XRP-USDT", "XRP", "USDT"),
	newSpotInstrument("SOL-USDT", "SOL", "USDT"),
	newSpotInstrument("ADA-USDT", "ADA", "USDT"),
	newSpotInstrument("DOGE-USDT", "DOGE", "USDT"),
	newSpotInstrument("BNB-USDT", "BNB", "USDT"),
	newSpotInstrument("LTC-USDT", "LTC", "USDT"),
	newSpotInstrument("DOT-USDT", "DOT", "USDT"),
	newSpotInstrument("AVAX-USDT", "AVAX", "USDT"),
}

const defaultInstrumentRefreshInterval = 30 * time.Minute

type nativeInstrument struct {
	symbol string
}

func (n nativeInstrument) Symbol() string {
	return n.symbol
}

type instrumentInterpreter struct{}

func (instrumentInterpreter) ToNative(inst schema.Instrument) (nativeInstrument, error) {
	if strings.TrimSpace(inst.Symbol) == "" {
		return nativeInstrument{symbol: ""}, fmt.Errorf("instrument symbol required")
	}
	symbol := normalizeInstrument(inst.Symbol)
	return nativeInstrument{symbol: symbol}, nil
}

func (instrumentInterpreter) FromNative(symbol string, catalog map[string]schema.Instrument) (schema.Instrument, error) {
	normalized := normalizeInstrument(symbol)
	inst, ok := catalog[normalized]
	if !ok {
		return schema.Instrument{}, fmt.Errorf("instrument %s not supported", normalized)
	}
	return inst, nil
}

type instrumentSupplier func() []nativeInstrument

func intPtr(v int) *int {
	value := v
	return &value
}

func newSpotInstrument(symbol, base, quote string) schema.Instrument {
	return schema.Instrument{
		Symbol:            symbol,
		Type:              schema.InstrumentTypeSpot,
		BaseCurrency:      base,
		QuoteCurrency:     quote,
		Venue:             "FAKE",
		Expiry:            "",
		ContractValue:     nil,
		ContractCurrency:  "",
		Strike:            nil,
		OptionType:        schema.OptionType(""),
		PriceIncrement:    "0.01",
		QuantityIncrement: "0.0001",
		PricePrecision:    intPtr(2),
		QuantityPrecision: intPtr(4),
		NotionalPrecision: intPtr(2),
		MinNotional:       "10",
		MinQuantity:       "0.0001",
		MaxQuantity:       "1000",
	}
}

// Options configures the fake provider runtime.
type Options struct {
	Name                      string
	TickerInterval            time.Duration
	TradeInterval             time.Duration
	BookSnapshotInterval      time.Duration
	Pools                     *pool.PoolManager
	Instruments               []schema.Instrument
	InstrumentRefreshInterval time.Duration
	InstrumentRefresh         func(context.Context) ([]schema.Instrument, error)
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

	instrumentMu              sync.RWMutex
	instrumentCodec           instrumentInterpreter
	instruments               map[string]schema.Instrument
	defaultNativeInstruments  []nativeInstrument
	instrumentRefreshInterval time.Duration
	instrumentRefresh         func(context.Context) ([]schema.Instrument, error)
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
	catalogue := append([]schema.Instrument(nil), opts.Instruments...)
	if len(catalogue) == 0 {
		catalogue = append(catalogue, DefaultInstruments...)
	}
	p.setSupportedInstruments(catalogue)

	refreshInterval := opts.InstrumentRefreshInterval
	if refreshInterval <= 0 {
		refreshInterval = defaultInstrumentRefreshInterval
	}
	p.instrumentRefreshInterval = refreshInterval
	if refreshInterval > 0 {
		if opts.InstrumentRefresh != nil {
			p.instrumentRefresh = opts.InstrumentRefresh
		} else {
			p.instrumentRefresh = func(context.Context) ([]schema.Instrument, error) {
				return schema.CloneInstruments(p.Instruments()), nil
			}
		}
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
	if p.instrumentRefreshInterval > 0 && p.instrumentRefresh != nil {
		go p.runInstrumentRefresh(ctx)
	}
	p.emitInstrumentCatalogue(p.Instruments())
	return nil
}

// Name returns the provider name.
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

// Instruments returns the current catalogue of supported instruments.
func (p *Provider) Instruments() []schema.Instrument {
	p.instrumentMu.RLock()
	defer p.instrumentMu.RUnlock()
	if len(p.defaultNativeInstruments) == 0 {
		return nil
	}
	out := make([]schema.Instrument, 0, len(p.defaultNativeInstruments))
	for _, native := range p.defaultNativeInstruments {
		inst, ok := p.instruments[native.symbol]
		if !ok {
			continue
		}
		out = append(out, schema.CloneInstrument(inst))
	}
	return out
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
	supplier := p.instrumentSupplier(route)
	handle.wg.Go(func() {
		p.runGenerator(routeCtx, evtType, supplier)
	})
	return handle
}

func (p *Provider) runGenerator(ctx context.Context, evtType schema.EventType, supply instrumentSupplier) {
	switch evtType {
	case schema.EventTypeTicker:
		p.streamTickers(ctx, supply)
	case schema.EventTypeTrade:
		p.streamTrades(ctx, supply)
	case schema.EventTypeBookSnapshot:
		p.streamBookSnapshots(ctx, supply)
	case schema.EventTypeExecReport, schema.EventTypeKlineSummary, schema.EventTypeControlAck, schema.EventTypeControlResult:
		<-ctx.Done()
	default:
		<-ctx.Done()
	}
}

func (p *Provider) instrumentSupplier(route dispatcher.Route) instrumentSupplier {
	filtered := p.instrumentsFromRoute(route)
	if len(filtered) == 0 {
		return p.currentDefaultNative
	}
	static := snapshotNative(filtered)
	return func() []nativeInstrument {
		return static
	}
}

func (p *Provider) instrumentsFromRoute(route dispatcher.Route) []nativeInstrument {
	if len(route.Filters) == 0 {
		return nil
	}
	set := make(map[string]struct{})
	for _, filter := range route.Filters {
		if strings.EqualFold(filter.Field, "instrument") {
			p.collectInstrumentValues(filter.Value, set)
		}
	}
	if len(set) == 0 {
		return nil
	}
	p.instrumentMu.RLock()
	defer p.instrumentMu.RUnlock()
	natives := make([]nativeInstrument, 0, len(set))
	for symbol := range set {
		if _, err := p.instrumentCodec.FromNative(symbol, p.instruments); err == nil {
			natives = append(natives, nativeInstrument{symbol: symbol})
		}
	}
	sort.Slice(natives, func(i, j int) bool { return natives[i].symbol < natives[j].symbol })
	return natives
}

func (p *Provider) collectInstrumentValues(value any, set map[string]struct{}) {
	switch v := value.(type) {
	case string:
		if symbol := normalizeInstrument(v); symbol != "" {
			set[symbol] = struct{}{}
		}
	case []string:
		for _, entry := range v {
			p.collectInstrumentValues(entry, set)
		}
	case []any:
		for _, entry := range v {
			p.collectInstrumentValues(entry, set)
		}
	}
}

func (p *Provider) currentDefaultNative() []nativeInstrument {
	p.instrumentMu.RLock()
	defer p.instrumentMu.RUnlock()
	return snapshotNative(p.defaultNativeInstruments)
}

func (p *Provider) streamTickers(ctx context.Context, supply instrumentSupplier) {
	ticker := time.NewTicker(p.tickerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, inst := range supply() {
				p.emitTicker(inst)
			}
		}
	}
}

func (p *Provider) streamTrades(ctx context.Context, supply instrumentSupplier) {
	ticker := time.NewTicker(p.tradeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, inst := range supply() {
				p.emitTrade(inst)
			}
		}
	}
}

func (p *Provider) streamBookSnapshots(ctx context.Context, supply instrumentSupplier) {
	p.emitSnapshots(supply())
	ticker := time.NewTicker(p.bookSnapshotInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.emitSnapshots(supply())
		}
	}
}

func (p *Provider) emitSnapshots(instruments []nativeInstrument) {
	for _, inst := range instruments {
		p.emitBookSnapshot(inst)
	}
}

func (p *Provider) emitTicker(instrument nativeInstrument) {
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

func (p *Provider) emitTrade(instrument nativeInstrument) {
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
	tradeID := fmt.Sprintf("%s-%d", strings.ReplaceAll(instrument.Symbol(), "-", ""), seq)
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

func (p *Provider) emitBookSnapshot(instrument nativeInstrument) {
	state := p.getInstrumentState(instrument)
	ts := p.clock().UTC()
	seq := p.nextSeq(schema.EventTypeBookSnapshot, instrument)
	state.mu.Lock()
	state.reseedOrderBook()
	levelsBids := toPriceLevels(state.bids)
	levelsAsks := toPriceLevels(state.asks)
	state.hasSnapshot = true
	checksum := checksum(instrument.Symbol(), schema.EventTypeBookSnapshot, seq)
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

func (p *Provider) newEvent(evtType schema.EventType, instrument nativeInstrument, seq uint64, payload any, ts time.Time) *schema.Event {
	if ts.IsZero() {
		ts = p.clock().UTC()
	}
	symbol := normalizeInstrument(instrument.Symbol())
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
		order.Symbol = p.defaultInstrumentSymbol()
	}
	nativeInst, ok := p.nativeInstrumentForSymbol(order.Symbol)
	if !ok {
		order.Symbol = p.defaultInstrumentSymbol()
		nativeInst, ok = p.nativeInstrumentForSymbol(order.Symbol)
		if !ok {
			p.emitError(fmt.Errorf("fake provider %s: unsupported instrument %q", p.name, order.Symbol))
			return
		}
	}
	order.Symbol = nativeInst.Symbol()
	if order.Timestamp.IsZero() {
		order.Timestamp = p.clock().UTC()
	}

	seq := p.nextSeq(schema.EventTypeExecReport, nativeInst)
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
	evt := p.newEvent(schema.EventTypeExecReport, nativeInst, seq, payload, ts)
	if evt != nil {
		p.emitEvent(evt)
	}
}

func (p *Provider) defaultInstrumentSymbol() string {
	p.instrumentMu.RLock()
	defer p.instrumentMu.RUnlock()
	if len(p.defaultNativeInstruments) == 0 {
		return ""
	}
	return p.defaultNativeInstruments[0].Symbol()
}

func (p *Provider) nativeInstrumentForSymbol(symbol string) (nativeInstrument, bool) {
	normalized := normalizeInstrument(symbol)
	if normalized == "" {
		return nativeInstrument{symbol: ""}, false
	}
	p.instrumentMu.RLock()
	defer p.instrumentMu.RUnlock()
	if _, ok := p.instruments[normalized]; !ok {
		return nativeInstrument{symbol: ""}, false
	}
	return nativeInstrument{symbol: normalized}, true
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

func (p *Provider) nextSeq(evtType schema.EventType, instrument nativeInstrument) uint64 {
	key := fmt.Sprintf("%s|%s", evtType, instrument.Symbol())
	p.seqMu.Lock()
	seq := p.seq[key] + 1
	p.seq[key] = seq
	p.seqMu.Unlock()
	return seq
}

func (p *Provider) getInstrumentState(inst nativeInstrument) *instrumentState {
	symbol := inst.Symbol()
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

func snapshotNative(src []nativeInstrument) []nativeInstrument {
	if len(src) == 0 {
		return nil
	}
	out := make([]nativeInstrument, len(src))
	copy(out, src)
	return out
}

func (p *Provider) setSupportedInstruments(list []schema.Instrument) {
	catalog := make(map[string]schema.Instrument, len(list))
	seen := make(map[string]struct{}, len(list))
	natives := make([]nativeInstrument, 0, len(list))
	for _, inst := range list {
		clone := schema.CloneInstrument(inst)
		if err := clone.Validate(); err != nil {
			log.Printf("fake provider %s: dropping instrument %q: %v", p.name, inst.Symbol, err)
			continue
		}
		native, err := p.instrumentCodec.ToNative(clone)
		if err != nil {
			log.Printf("fake provider %s: normalize instrument %q failed: %v", p.name, inst.Symbol, err)
			continue
		}
		catalog[native.symbol] = clone
		if _, exists := seen[native.symbol]; !exists {
			seen[native.symbol] = struct{}{}
			natives = append(natives, native)
		}
	}
	if len(catalog) == 0 {
		log.Printf("fake provider %s: instrument catalogue empty; retaining previous set", p.name)
		return
	}
	sort.Slice(natives, func(i, j int) bool { return natives[i].symbol < natives[j].symbol })
	ordered := make([]schema.Instrument, 0, len(natives))
	for _, native := range natives {
		if inst, ok := catalog[native.symbol]; ok {
			ordered = append(ordered, schema.CloneInstrument(inst))
		}
	}
	p.instrumentMu.Lock()
	p.instruments = catalog
	p.defaultNativeInstruments = natives
	p.instrumentMu.Unlock()
	log.Printf("fake provider %s: instrument catalogue updated: %d instruments", p.name, len(ordered))
	p.emitInstrumentCatalogue(ordered)
}

func (p *Provider) runInstrumentRefresh(ctx context.Context) {
	ticker := time.NewTicker(p.instrumentRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.refreshInstruments(ctx)
		}
	}
}

func (p *Provider) refreshInstruments(ctx context.Context) {
	if p.instrumentRefresh == nil {
		return
	}
	list, err := p.instrumentRefresh(ctx)
	if err != nil {
		p.emitError(fmt.Errorf("instrument refresh: %w", err))
		return
	}
	if len(list) == 0 {
		return
	}
	p.setSupportedInstruments(list)
}

func (p *Provider) emitInstrumentCatalogue(list []schema.Instrument) {
	if len(list) == 0 {
		list = []schema.Instrument{}
	}
	seq := p.nextInstrumentSeq()
	now := p.clock().UTC()
	snapshot := schema.CloneInstruments(list)
	if snapshot == nil {
		snapshot = []schema.Instrument{}
	}
	inst := schema.InstrumentUpdatePayload{Instruments: snapshot}
	evt := p.borrowEvent(p.ctx)
	if evt == nil {
		return
	}
	evt.EventID = fmt.Sprintf("%s:INSTRUMENTS:%d", p.name, seq)
	evt.Provider = p.name
	evt.Symbol = ""
	evt.Type = schema.EventTypeInstrumentUpdate
	evt.SeqProvider = seq
	evt.IngestTS = now
	evt.EmitTS = now
	evt.Payload = inst
	p.emitEvent(evt)
}

func (p *Provider) nextInstrumentSeq() uint64 {
	p.seqMu.Lock()
	defer p.seqMu.Unlock()
	key := fmt.Sprintf("%s|%s", schema.EventTypeInstrumentUpdate, "ALL")
	seq := p.seq[key] + 1
	p.seq[key] = seq
	return seq
}

func normalizeInstrument(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
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
