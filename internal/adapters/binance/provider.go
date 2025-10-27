package binance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/coder/websocket"
	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/adapters/shared"
	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

// Provider implements the Binance spot market adapter.
type Provider struct {
	name   string
	opts   Options
	pools  *pool.PoolManager
	clock  func() time.Time
	client *http.Client

	events chan *schema.Event
	errs   chan error

	ctx    context.Context
	cancel context.CancelFunc

	started atomic.Bool

	publisher *shared.Publisher

	instrumentsMu sync.RWMutex
	instruments   map[string]schema.Instrument
	symbols       map[string]symbolMeta // canonical symbol -> meta
	restToCanon   map[string]string     // REST symbol -> canonical

	tradeMu   sync.Mutex
	tradeSubs map[string]*streamHandle

	tickerMu   sync.Mutex
	tickerSubs map[string]*streamHandle

	bookMu   sync.Mutex
	bookSubs map[string]*bookHandle
}

type streamHandle struct {
	cancel context.CancelFunc
}

type bookHandle struct {
	cancel    context.CancelFunc
	assembler *shared.OrderBookAssembler
	seqMu     sync.Mutex
	lastSeq   uint64
}

var errOrderbookOutOfSync = errors.New("orderbook out of sync")

// NewProvider constructs a Binance provider instance.
func NewProvider(opts Options) *Provider {
	opts = withDefaults(opts)
	p := &Provider{
		name:        opts.Name,
		opts:        opts,
		pools:       opts.Pools,
		clock:       time.Now,
		events:      make(chan *schema.Event, 2048),
		errs:        make(chan error, 32),
		instruments: make(map[string]schema.Instrument),
		symbols:     make(map[string]symbolMeta),
		restToCanon: make(map[string]string),
		tradeSubs:   make(map[string]*streamHandle),
		tickerSubs:  make(map[string]*streamHandle),
		bookSubs:    make(map[string]*bookHandle),
	}
	if p.pools == nil {
		pm := pool.NewPoolManager()
		_ = pm.RegisterPool("Event", 1024, func() any { return new(schema.Event) })
		p.pools = pm
	}
	p.publisher = shared.NewPublisher(p.name, p.events, p.pools, p.clock)
	return p
}

// Name returns the configured provider name.
func (p *Provider) Name() string { return p.name }

// Events exposes the event stream.
func (p *Provider) Events() <-chan *schema.Event { return p.events }

// Errors exposes asynchronous provider errors.
func (p *Provider) Errors() <-chan error { return p.errs }

// Start activates the provider until the context is cancelled.
func (p *Provider) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("binance provider requires context")
	}
	if !p.started.CompareAndSwap(false, true) {
		return errors.New("binance provider already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	p.ctx = runCtx
	p.cancel = cancel

	if err := p.refreshInstruments(runCtx); err != nil {
		p.started.Store(false)
		cancel()
		return fmt.Errorf("seed instruments: %w", err)
	}

	go p.instrumentRefreshLoop()

	go func() {
		<-runCtx.Done()
		p.stopAllStreams()
		close(p.events)
		close(p.errs)
	}()

	return nil
}

// SubmitOrder proxies order submissions; currently unsupported.
func (p *Provider) SubmitOrder(_ context.Context, _ schema.OrderRequest) error {
	return errors.New("binance provider does not support order submission")
}

// Instruments returns the cached instrument catalogue.
func (p *Provider) Instruments() []schema.Instrument {
	p.instrumentsMu.RLock()
	defer p.instrumentsMu.RUnlock()
	out := make([]schema.Instrument, 0, len(p.instruments))
	for _, inst := range p.instruments {
		out = append(out, schema.CloneInstrument(inst))
	}
	return out
}

// SubscribeRoute activates streaming for the specified route.
func (p *Provider) SubscribeRoute(route dispatcher.Route) error {
	if err := p.ensureRunning(); err != nil {
		return err
	}
	instruments := extractInstruments(route.Filters)
	switch route.Type {
	case schema.RouteTypeTrade:
		return p.configureTradeStreams(instruments)
	case schema.RouteTypeTicker:
		return p.configureTickerStreams(instruments)
	case schema.RouteTypeOrderbookSnapshot:
		return p.configureOrderBookStreams(instruments)
	default:
		// Unsupported routes are acknowledged but inert.
		return nil
	}
}

// UnsubscribeRoute tears down streaming for the provided route type.
func (p *Provider) UnsubscribeRoute(routeType schema.RouteType) error {
	switch routeType {
	case schema.RouteTypeTrade:
		p.stopTradeStreams()
	case schema.RouteTypeTicker:
		p.stopTickerStreams()
	case schema.RouteTypeOrderbookSnapshot:
		p.stopOrderBookStreams()
	default:
	}
	return nil
}

func (p *Provider) ensureRunning() error {
	if !p.started.Load() || p.ctx == nil {
		return errors.New("binance provider not started")
	}
	return nil
}

func (p *Provider) httpClient() *http.Client {
	if p.client == nil {
		p.client = &http.Client{Timeout: p.opts.HTTPTimeout}
	}
	return p.client
}

func (p *Provider) refreshInstruments(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, p.opts.HTTPTimeout)
	defer cancel()
	list, metas, err := p.fetchExchangeInfo(ctx)
	if err != nil {
		return err
	}
	p.instrumentsMu.Lock()
	p.instruments = make(map[string]schema.Instrument, len(list))
	p.symbols = metas
	p.restToCanon = make(map[string]string, len(metas))
	for _, inst := range list {
		cloned := schema.CloneInstrument(inst)
		p.instruments[inst.Symbol] = cloned
	}
	for canonical, meta := range metas {
		p.restToCanon[meta.rest] = canonical
	}
	p.instrumentsMu.Unlock()
	return nil
}

func (p *Provider) instrumentRefreshLoop() {
	ticker := time.NewTicker(p.opts.InstrumentRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			if err := p.refreshInstruments(p.ctx); err != nil {
				p.reportError(fmt.Errorf("refresh instruments: %w", err))
				continue
			}
			p.publishInstrumentUpdates()
		}
	}
}

func (p *Provider) publishInstrumentUpdates() {
	p.instrumentsMu.RLock()
	defer p.instrumentsMu.RUnlock()
	for _, inst := range p.instruments {
		payload := schema.InstrumentUpdatePayload{Instrument: schema.CloneInstrument(inst)}
		p.publisher.PublishInstrumentUpdate(p.ctx, inst.Symbol, payload)
	}
}

func extractInstruments(filters []dispatcher.FilterRule) []string {
	symbols := make(map[string]struct{})
	for _, filter := range filters {
		if !strings.EqualFold(filter.Field, "instrument") {
			continue
		}
		switch v := filter.Value.(type) {
		case string:
			if trimmed := strings.ToUpper(strings.TrimSpace(v)); trimmed != "" {
				symbols[trimmed] = struct{}{}
			}
		case []string:
			for _, entry := range v {
				if trimmed := strings.ToUpper(strings.TrimSpace(entry)); trimmed != "" {
					symbols[trimmed] = struct{}{}
				}
			}
		}
	}
	out := make([]string, 0, len(symbols))
	for symbol := range symbols {
		out = append(out, symbol)
	}
	if len(out) == 0 {
		// Ensure we don't accidentally activate every instrument.
		return nil
	}
	return out
}

func (p *Provider) metaForInstrument(symbol string) (symbolMeta, bool) {
	p.instrumentsMu.RLock()
	defer p.instrumentsMu.RUnlock()
	meta, ok := p.symbols[symbol]
	return meta, ok
}

func (p *Provider) configureTradeStreams(instruments []string) error {
	p.tradeMu.Lock()
	defer p.tradeMu.Unlock()
	p.stopTradeLocked()
	for _, inst := range instruments {
		meta, ok := p.metaForInstrument(inst)
		if !ok {
			p.reportError(fmt.Errorf("trade stream instrument not found: %s", inst))
			continue
		}
		ctx, cancel := context.WithCancel(p.ctx)
		handle := &streamHandle{cancel: cancel}
		p.tradeSubs[inst] = handle
		go p.runTradeStream(ctx, meta)
	}
	return nil
}

func (p *Provider) configureTickerStreams(instruments []string) error {
	p.tickerMu.Lock()
	defer p.tickerMu.Unlock()
	p.stopTickerLocked()
	for _, inst := range instruments {
		meta, ok := p.metaForInstrument(inst)
		if !ok {
			p.reportError(fmt.Errorf("ticker stream instrument not found: %s", inst))
			continue
		}
		ctx, cancel := context.WithCancel(p.ctx)
		handle := &streamHandle{cancel: cancel}
		p.tickerSubs[inst] = handle
		go p.runTickerStream(ctx, meta)
	}
	return nil
}

func (p *Provider) configureOrderBookStreams(instruments []string) error {
	p.bookMu.Lock()
	defer p.bookMu.Unlock()
	p.stopOrderBookLocked()
	for _, inst := range instruments {
		meta, ok := p.metaForInstrument(inst)
		if !ok {
			p.reportError(fmt.Errorf("orderbook stream instrument not found: %s", inst))
			continue
		}
		ctx, cancel := context.WithCancel(p.ctx)
		handle := &bookHandle{
			cancel:    cancel,
			assembler: shared.NewOrderBookAssembler(p.opts.SnapshotDepth),
		}
		p.bookSubs[inst] = handle
		go p.runOrderBookStream(ctx, meta, handle)
	}
	return nil
}

func (p *Provider) stopTradeStreams() {
	p.tradeMu.Lock()
	defer p.tradeMu.Unlock()
	p.stopTradeLocked()
}

func (p *Provider) stopTradeLocked() {
	for key, handle := range p.tradeSubs {
		if handle != nil && handle.cancel != nil {
			handle.cancel()
		}
		delete(p.tradeSubs, key)
	}
}

func (p *Provider) stopTickerStreams() {
	p.tickerMu.Lock()
	defer p.tickerMu.Unlock()
	p.stopTickerLocked()
}

func (p *Provider) stopTickerLocked() {
	for key, handle := range p.tickerSubs {
		if handle != nil && handle.cancel != nil {
			handle.cancel()
		}
		delete(p.tickerSubs, key)
	}
}

func (p *Provider) stopOrderBookStreams() {
	p.bookMu.Lock()
	defer p.bookMu.Unlock()
	p.stopOrderBookLocked()
}

func (p *Provider) stopOrderBookLocked() {
	for key, handle := range p.bookSubs {
		if handle != nil && handle.cancel != nil {
			handle.cancel()
		}
		delete(p.bookSubs, key)
	}
}

func (p *Provider) stopAllStreams() {
	p.stopTradeStreams()
	p.stopTickerStreams()
	p.stopOrderBookStreams()
}

func (p *Provider) runTradeStream(ctx context.Context, meta symbolMeta) {
	stream := meta.stream + "@trade"
	handler := func(msg []byte) error {
		var event tradeMessage
		if err := json.Unmarshal(msg, &event); err != nil {
			return fmt.Errorf("decode trade message: %w", err)
		}
		payload := schema.TradePayload{
			TradeID:   fmt.Sprintf("%s-%d", meta.canonical, event.TradeID),
			Side:      tradeSideFromAggressor(event.IsBuyerMaker),
			Price:     event.Price,
			Quantity:  event.Quantity,
			Timestamp: time.UnixMilli(event.TradeTime).UTC(),
		}
		p.publisher.PublishTrade(p.ctx, meta.canonical, payload)
		return nil
	}
	p.consumeStream(ctx, stream, handler)
}

func (p *Provider) runTickerStream(ctx context.Context, meta symbolMeta) {
	stream := meta.stream + "@ticker"
	handler := func(msg []byte) error {
		var event tickerMessage
		if err := json.Unmarshal(msg, &event); err != nil {
			return fmt.Errorf("decode ticker message: %w", err)
		}
		payload := schema.TickerPayload{
			LastPrice: event.LastPrice,
			BidPrice:  event.BidPrice,
			AskPrice:  event.AskPrice,
			Volume24h: event.Volume,
			Timestamp: time.UnixMilli(event.EventTime).UTC(),
		}
		p.publisher.PublishTicker(p.ctx, meta.canonical, payload)
		return nil
	}
	p.consumeStream(ctx, stream, handler)
}

func (p *Provider) runOrderBookStream(ctx context.Context, meta symbolMeta, handle *bookHandle) {
	backoffCfg := backoff.NewExponentialBackOff()
	stream := meta.stream + "@depth@100ms"

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		base := strings.TrimSuffix(p.opts.WebsocketBaseURL, "/")
		url := base + "/" + stream
		conn, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			p.reportError(fmt.Errorf("dial %s: %w", url, err))
			sleep := backoffCfg.NextBackOff()
			select {
			case <-ctx.Done():
				return
			case <-time.After(sleep):
				continue
			}
		}

		if err := p.seedOrderBook(ctx, meta, handle); err != nil {
			p.reportError(fmt.Errorf("seed orderbook %s: %w", meta.canonical, err))
			_ = conn.Close(websocket.StatusInternalError, "seed error")
			sleep := backoffCfg.NextBackOff()
			select {
			case <-ctx.Done():
				return
			case <-time.After(sleep):
				continue
			}
		}

		backoffCfg.Reset()

		reconnect := false
		for {
			msgType, data, err := conn.Read(ctx)
			if err != nil {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					p.reportError(fmt.Errorf("read depth %s: %w", url, err))
				}
				reconnect = true
				break
			}
			if msgType != websocket.MessageText {
				continue
			}
			var diff depthDiffMessage
			if err := json.Unmarshal(data, &diff); err != nil {
				p.reportError(fmt.Errorf("decode depth message: %w", err))
				continue
			}
			if err := p.applyDepthDiff(meta, handle, diff); err != nil {
				if errors.Is(err, errOrderbookOutOfSync) {
					reconnect = true
					break
				}
				p.reportError(fmt.Errorf("apply depth diff %s: %w", meta.canonical, err))
			}
		}

		_ = conn.Close(websocket.StatusNormalClosure, "resync")

		if reconnect {
			sleep := backoffCfg.NextBackOff()
			select {
			case <-ctx.Done():
				return
			case <-time.After(sleep):
			}
		}
	}
}

func (p *Provider) seedOrderBook(ctx context.Context, meta symbolMeta, handle *bookHandle) error {
	snapshotCtx, cancel := context.WithTimeout(ctx, p.opts.HTTPTimeout)
	defer cancel()
	snapshot, err := p.fetchDepthSnapshot(snapshotCtx, meta.rest)
	if err != nil {
		return err
	}
	payload := schema.BookSnapshotPayload{
		Bids:          levelsToPriceLevels(snapshot.Bids),
		Asks:          levelsToPriceLevels(snapshot.Asks),
		LastUpdate:    time.Now().UTC(),
		FirstUpdateID: uint64(snapshot.LastUpdateID),
		FinalUpdateID: uint64(snapshot.LastUpdateID),
	}
	_, err = handle.assembler.ApplySnapshot(uint64(snapshot.LastUpdateID), payload)
	if err != nil {
		return err
	}
	handle.seqMu.Lock()
	handle.lastSeq = uint64(snapshot.LastUpdateID)
	handle.seqMu.Unlock()
	p.publisher.PublishBookSnapshot(p.ctx, meta.canonical, payload)
	return nil
}

func (p *Provider) applyDepthDiff(meta symbolMeta, handle *bookHandle, diff depthDiffMessage) error {
	if diff.FinalUpdateID == 0 {
		return nil
	}
	handle.seqMu.Lock()
	lastSeq := handle.lastSeq
	handle.seqMu.Unlock()

	if diff.FinalUpdateID <= lastSeq {
		return nil
	}
	if diff.FirstUpdateID > lastSeq+1 {
		handle.seqMu.Lock()
		handle.lastSeq = 0
		handle.seqMu.Unlock()
		return errOrderbookOutOfSync
	}
	if diff.FinalUpdateID < lastSeq+1 {
		return nil
	}

	update := shared.OrderBookDiff{
		SequenceID: diff.FinalUpdateID,
		Bids:       toDiffLevels(diff.Bids),
		Asks:       toDiffLevels(diff.Asks),
		Timestamp:  time.UnixMilli(diff.EventTime).UTC(),
	}
	snapshot, applied, err := handle.assembler.ApplyDiff(update)
	if err != nil {
		return err
	}
	if !applied {
		return nil
	}
	handle.seqMu.Lock()
	handle.lastSeq = diff.FinalUpdateID
	handle.seqMu.Unlock()
	snapshot.FirstUpdateID = diff.FirstUpdateID
	snapshot.FinalUpdateID = diff.FinalUpdateID
	p.publisher.PublishBookSnapshot(p.ctx, meta.canonical, snapshot)
	return nil
}

func (p *Provider) consumeStream(ctx context.Context, stream string, handler func([]byte) error) error {
	base := strings.TrimSuffix(p.opts.WebsocketBaseURL, "/")
	url := base + "/" + stream
	backoffCfg := backoff.NewExponentialBackOff()

	for {
		select {
		case <-ctx.Done():
			return context.Canceled
		default:
		}

		conn, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			p.reportError(fmt.Errorf("dial %s: %w", url, err))
			sleep := backoffCfg.NextBackOff()
			select {
			case <-ctx.Done():
				return context.Canceled
			case <-time.After(sleep):
				continue
			}
		}

		backoffCfg.Reset()
		readCtx := ctx
		for {
			msgType, data, err := conn.Read(readCtx)
			if err != nil {
				_ = conn.Close(websocket.StatusNormalClosure, "")
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return context.Canceled
				}
				p.reportError(fmt.Errorf("read %s: %w", url, err))
				break
			}
			if msgType != websocket.MessageText {
				continue
			}
			if err := handler(data); err != nil {
				p.reportError(fmt.Errorf("handle %s: %w", url, err))
			}
		}

		sleep := backoffCfg.NextBackOff()
		select {
		case <-ctx.Done():
			return context.Canceled
		case <-time.After(sleep):
		}
	}
}

func (p *Provider) reportError(err error) {
	if err == nil {
		return
	}
	select {
	case <-p.ctx.Done():
	case p.errs <- err:
	default:
	}
}

func tradeSideFromAggressor(isBuyerMaker bool) schema.TradeSide {
	if isBuyerMaker {
		return schema.TradeSideSell
	}
	return schema.TradeSideBuy
}

type tradeMessage struct {
	EventTime    int64  `json:"E"`
	Symbol       string `json:"s"`
	TradeID      int64  `json:"t"`
	Price        string `json:"p"`
	Quantity     string `json:"q"`
	TradeTime    int64  `json:"T"`
	IsBuyerMaker bool   `json:"m"`
}

type tickerMessage struct {
	EventTime int64  `json:"E"`
	Symbol    string `json:"s"`
	LastPrice string `json:"c"`
	BidPrice  string `json:"b"`
	AskPrice  string `json:"a"`
	Volume    string `json:"v"`
}

type depthDiffMessage struct {
	EventTime     int64      `json:"E"`
	Symbol        string     `json:"s"`
	FirstUpdateID uint64     `json:"U"`
	FinalUpdateID uint64     `json:"u"`
	Bids          [][]string `json:"b"`
	Asks          [][]string `json:"a"`
}

func levelsToPriceLevels(levels [][]string) []schema.PriceLevel {
	if len(levels) == 0 {
		return nil
	}
	out := make([]schema.PriceLevel, 0, len(levels))
	for _, level := range levels {
		if len(level) < 2 {
			continue
		}
		out = append(out, schema.PriceLevel{
			Price:    level[0],
			Quantity: level[1],
		})
	}
	return out
}

func toDiffLevels(levels [][]string) []shared.DiffLevel {
	if len(levels) == 0 {
		return nil
	}
	out := make([]shared.DiffLevel, 0, len(levels))
	for _, level := range levels {
		if len(level) < 2 {
			continue
		}
		out = append(out, shared.DiffLevel{Price: level[0], Quantity: level[1]})
	}
	return out
}
