package binance

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/coder/websocket"
	"github.com/goccy/go-json"
	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/app/dispatcher"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/adapters/shared"
	"github.com/coachpo/meltica/internal/infra/pool"
	"github.com/coachpo/meltica/internal/infra/telemetry"
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
	metrics   *providerMetrics

	instrumentsMu sync.RWMutex
	instruments   map[string]schema.Instrument
	symbols       map[string]symbolMeta // canonical symbol -> meta
	restToCanon   map[string]string     // REST symbol -> canonical

	tradeMu      sync.Mutex
	tradeManager *streamManager

	tickerMu      sync.Mutex
	tickerManager *streamManager

	bookMu      sync.Mutex
	bookManager *streamManager
	bookHandles map[string]*bookHandle

	userStreamMu     sync.Mutex
	userStreamCancel context.CancelFunc
	userStreamWG     sync.WaitGroup

	balanceMu sync.Mutex
	balances  map[string]balanceSnapshot
}

type bookHandle struct {
	assembler *shared.OrderBookAssembler
	seqMu     sync.Mutex
	lastSeq   uint64
	seeded    atomic.Bool
	seeding   atomic.Bool
	bufferMu  sync.Mutex
	buffer    []depthDiffMessage
}

var errOrderbookOutOfSync = errors.New("orderbook out of sync")

func (h *bookHandle) appendDiff(diff depthDiffMessage) {
	h.bufferMu.Lock()
	h.buffer = append(h.buffer, diff)
	h.bufferMu.Unlock()
}

func (h *bookHandle) firstBufferedUpdateID() uint64 {
	h.bufferMu.Lock()
	defer h.bufferMu.Unlock()
	if len(h.buffer) == 0 {
		return 0
	}
	return h.buffer[0].FirstUpdateID
}

func (h *bookHandle) drainBuffered(after uint64) []depthDiffMessage {
	h.bufferMu.Lock()
	defer h.bufferMu.Unlock()
	if len(h.buffer) == 0 {
		return nil
	}
	drop := 0
	for drop < len(h.buffer) && h.buffer[drop].FinalUpdateID <= after {
		drop++
	}
	if drop > 0 {
		copy(h.buffer, h.buffer[drop:])
		h.buffer = h.buffer[:len(h.buffer)-drop]
	}
	if len(h.buffer) == 0 {
		return nil
	}
	out := make([]depthDiffMessage, len(h.buffer))
	copy(out, h.buffer)
	h.buffer = h.buffer[:0]
	return out
}

func (h *bookHandle) requeueDiffs(diffs []depthDiffMessage) {
	if len(diffs) == 0 {
		return
	}
	h.bufferMu.Lock()
	defer h.bufferMu.Unlock()
	h.buffer = append(diffs, h.buffer...)
}

func (h *bookHandle) currentSeq() uint64 {
	h.seqMu.Lock()
	defer h.seqMu.Unlock()
	return h.lastSeq
}

// NewProvider constructs a Binance provider instance.
func NewProvider(opts Options) *Provider {
	opts = withDefaults(opts)
	p := &Provider{
		name:             opts.Name,
		opts:             opts,
		pools:            opts.Pools,
		clock:            time.Now,
		client:           nil,
		events:           make(chan *schema.Event, 2048),
		errs:             make(chan error, 32),
		ctx:              nil,
		cancel:           nil,
		started:          atomic.Bool{},
		publisher:        nil,
		metrics:          nil,
		instrumentsMu:    sync.RWMutex{},
		instruments:      make(map[string]schema.Instrument),
		symbols:          make(map[string]symbolMeta),
		restToCanon:      make(map[string]string),
		tradeMu:          sync.Mutex{},
		tradeManager:     nil,
		tickerMu:         sync.Mutex{},
		tickerManager:    nil,
		bookMu:           sync.Mutex{},
		bookManager:      nil,
		bookHandles:      make(map[string]*bookHandle),
		userStreamMu:     sync.Mutex{},
		userStreamCancel: nil,
		userStreamWG:     sync.WaitGroup{},
		balanceMu:        sync.Mutex{},
		balances:         make(map[string]balanceSnapshot),
	}
	if p.pools == nil {
		log.Printf("binance/provider: Pools not injected; provider cannot start without shared PoolManager")
		panic("binance/provider: nil PoolManager in options")
	}
	p.publisher = shared.NewPublisher(p.name, p.events, p.pools, p.clock)
	p.balances = make(map[string]balanceSnapshot)
	p.metrics = newProviderMetrics(p)
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

	// Initialize stream managers for live subscribe/unsubscribe
	if err := p.initStreamManagers(runCtx); err != nil {
		p.started.Store(false)
		cancel()
		return fmt.Errorf("init stream managers: %w", err)
	}

	if p.hasTradingCredentials() {
		p.startUserDataStream()
	}

	go func() {
		<-runCtx.Done()
		p.stopAllStreams()
		close(p.events)
		close(p.errs)
	}()

	return nil
}

// SubmitOrder proxies order submissions; currently unsupported.
func (p *Provider) SubmitOrder(ctx context.Context, req schema.OrderRequest) error {
	if err := p.ensureRunning(); err != nil {
		return err
	}
	meta, ok := p.metaForInstrument(req.Symbol)
	if !ok {
		if err := p.refreshInstruments(p.ctx); err == nil {
			meta, ok = p.metaForInstrument(req.Symbol)
		}
	}
	if !ok {
		return fmt.Errorf("binance: instrument %s not found", strings.TrimSpace(req.Symbol))
	}
	if strings.TrimSpace(p.opts.APIKey) == "" || strings.TrimSpace(p.opts.APISecret) == "" {
		return fmt.Errorf("binance: trading disabled (api credentials missing)")
	}
	if ctx == nil {
		ctx = p.ctx
	}
	return p.submitOrder(ctx, meta, req)
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
	instruments := extractInstruments(route.Filters, p.name)
	switch route.Type {
	case schema.RouteTypeTrade:
		return p.configureTradeStreams(instruments)
	case schema.RouteTypeTicker:
		return p.configureTickerStreams(instruments)
	case schema.RouteTypeOrderbookSnapshot:
		return p.configureOrderBookStreams(instruments)
	case schema.RouteTypeAccountBalance,
		schema.RouteTypeExecutionReport,
		schema.RouteTypeKlineSummary,
		schema.RouteTypeInstrumentUpdate,
		schema.RouteTypeRiskControl:
		return nil
	default:
		// Unsupported routes are acknowledged but inert.
		return nil
	}
}

// UnsubscribeRoute tears down streaming for the provided route type.
func (p *Provider) UnsubscribeRoute(route dispatcher.Route) error {
	if err := p.ensureRunning(); err != nil {
		return err
	}
	instruments := extractInstruments(route.Filters, p.name)
	switch route.Type {
	case schema.RouteTypeTrade:
		return p.unsubscribeTradeStreams(instruments)
	case schema.RouteTypeTicker:
		return p.unsubscribeTickerStreams(instruments)
	case schema.RouteTypeOrderbookSnapshot:
		return p.unsubscribeOrderBookStreams(instruments)
	case schema.RouteTypeAccountBalance,
		schema.RouteTypeExecutionReport,
		schema.RouteTypeKlineSummary,
		schema.RouteTypeInstrumentUpdate,
		schema.RouteTypeRiskControl:
		return nil
	default:
		return nil
	}
}

func (p *Provider) ensureRunning() error {
	if !p.started.Load() || p.ctx == nil {
		return errors.New("binance provider not started")
	}
	return nil
}

func (p *Provider) httpClient() *http.Client {
	if p.client == nil {
		client := &http.Client{
			Transport:     nil,
			CheckRedirect: nil,
			Jar:           nil,
			Timeout:       p.opts.httpTimeoutDuration(),
		}
		p.client = client
	}
	return p.client
}

func (p *Provider) refreshInstruments(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, p.opts.httpTimeoutDuration())
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
	ticker := time.NewTicker(p.opts.instrumentRefreshDuration())
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
		if p.metrics != nil {
			p.metrics.recordEvent(p.ctx, telemetry.EventTypeInstrumentUpdate, inst.Symbol)
		}
	}
}

func providerInstrumentField(provider string) string {
	trimmed := strings.TrimSpace(provider)
	if trimmed == "" {
		return "instrument"
	}
	return "instrument@" + strings.ToLower(trimmed)
}

func extractInstruments(filters []dispatcher.FilterRule, provider string) []string {
	global := make(map[string]struct{})
	specific := make(map[string]struct{})
	providerField := providerInstrumentField(provider)
	addValues := func(target map[string]struct{}, value any) {
		switch v := value.(type) {
		case string:
			if trimmed := strings.ToUpper(strings.TrimSpace(v)); trimmed != "" {
				target[trimmed] = struct{}{}
			}
		case []string:
			for _, entry := range v {
				if trimmed := strings.ToUpper(strings.TrimSpace(entry)); trimmed != "" {
					target[trimmed] = struct{}{}
				}
			}
		case []any:
			for _, entry := range v {
				if trimmed := strings.ToUpper(strings.TrimSpace(fmt.Sprint(entry))); trimmed != "" {
					target[trimmed] = struct{}{}
				}
			}
		}
	}

	for _, filter := range filters {
		field := strings.TrimSpace(filter.Field)
		switch {
		case strings.EqualFold(field, providerField):
			addValues(specific, filter.Value)
		case strings.EqualFold(field, "instrument"):
			addValues(global, filter.Value)
		}
	}

	source := specific
	if len(source) == 0 {
		source = global
	}

	out := make([]string, 0, len(source))
	for symbol := range source {
		out = append(out, symbol)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (p *Provider) metaForInstrument(symbol string) (symbolMeta, bool) {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	p.instrumentsMu.RLock()
	defer p.instrumentsMu.RUnlock()
	meta, ok := p.symbols[normalized]
	return meta, ok
}

func (p *Provider) configureTradeStreams(instruments []string) error {
	p.tradeMu.Lock()
	defer p.tradeMu.Unlock()

	if p.tradeManager == nil {
		return errors.New("trade stream manager not initialized")
	}

	streams := make([]string, 0, len(instruments))
	for _, inst := range instruments {
		meta, ok := p.metaForInstrument(inst)
		if !ok {
			p.reportError(fmt.Errorf("trade stream instrument not found: %s", inst))
			continue
		}
		streams = append(streams, meta.stream+"@trade")
	}

	if len(streams) > 0 {
		return p.tradeManager.subscribe(streams)
	}
	return nil
}

func (p *Provider) unsubscribeTradeStreams(instruments []string) error {
	p.tradeMu.Lock()
	defer p.tradeMu.Unlock()

	if p.tradeManager == nil {
		return nil
	}

	streams := make([]string, 0, len(instruments))
	for _, inst := range instruments {
		meta, ok := p.metaForInstrument(inst)
		if !ok {
			continue
		}
		streams = append(streams, meta.stream+"@trade")
	}

	if len(streams) == 0 {
		return nil
	}

	return p.tradeManager.unsubscribe(streams)
}

func (p *Provider) configureTickerStreams(instruments []string) error {
	p.tickerMu.Lock()
	defer p.tickerMu.Unlock()

	if p.tickerManager == nil {
		return errors.New("ticker stream manager not initialized")
	}

	streams := make([]string, 0, len(instruments))
	for _, inst := range instruments {
		meta, ok := p.metaForInstrument(inst)
		if !ok {
			p.reportError(fmt.Errorf("ticker stream instrument not found: %s", inst))
			continue
		}
		streams = append(streams, meta.stream+"@ticker")
	}

	if len(streams) > 0 {
		return p.tickerManager.subscribe(streams)
	}
	return nil
}

func (p *Provider) unsubscribeTickerStreams(instruments []string) error {
	p.tickerMu.Lock()
	defer p.tickerMu.Unlock()

	if p.tickerManager == nil {
		return nil
	}

	streams := make([]string, 0, len(instruments))
	for _, inst := range instruments {
		meta, ok := p.metaForInstrument(inst)
		if !ok {
			continue
		}
		streams = append(streams, meta.stream+"@ticker")
	}

	if len(streams) == 0 {
		return nil
	}

	return p.tickerManager.unsubscribe(streams)
}

func (p *Provider) configureOrderBookStreams(instruments []string) error {
	p.bookMu.Lock()
	defer p.bookMu.Unlock()

	if p.bookManager == nil {
		return errors.New("orderbook stream manager not initialized")
	}

	streams := make([]string, 0, len(instruments))
	for _, inst := range instruments {
		meta, ok := p.metaForInstrument(inst)
		if !ok {
			p.reportError(fmt.Errorf("orderbook stream instrument not found: %s", inst))
			continue
		}

		// Create book handle if not exists
		if _, exists := p.bookHandles[meta.canonical]; !exists {
			handle := &bookHandle{
				assembler: shared.NewOrderBookAssembler(p.opts.SnapshotDepth),
				seqMu:     sync.Mutex{},
				lastSeq:   0,
				seeded:    atomic.Bool{},
				seeding:   atomic.Bool{},
				bufferMu:  sync.Mutex{},
				buffer:    nil,
			}
			p.bookHandles[meta.canonical] = handle
		}

		streams = append(streams, meta.stream+"@depth@100ms")
	}

	if len(streams) > 0 {
		return p.bookManager.subscribe(streams)
	}
	return nil
}

func (p *Provider) unsubscribeOrderBookStreams(instruments []string) error {
	p.bookMu.Lock()
	defer p.bookMu.Unlock()

	if p.bookManager == nil {
		return nil
	}

	streams := make([]string, 0, len(instruments))
	for _, inst := range instruments {
		meta, ok := p.metaForInstrument(inst)
		if !ok {
			continue
		}
		streams = append(streams, meta.stream+"@depth@100ms")
		delete(p.bookHandles, meta.canonical)
	}

	if len(streams) == 0 {
		return nil
	}

	return p.bookManager.unsubscribe(streams)
}

func (p *Provider) stopAllStreams() {
	if p.tradeManager != nil {
		p.tradeManager.stop()
	}
	if p.tickerManager != nil {
		p.tickerManager.stop()
	}
	if p.bookManager != nil {
		p.bookManager.stop()
	}
	p.stopUserDataStream()
}

func (p *Provider) initStreamManagers(ctx context.Context) error {
	baseURL := strings.TrimSuffix(p.opts.websocketURL(), "/") + "/ws"

	// Trade stream handler
	tradeHandler := func(data []byte) error {
		var event tradeMessage
		if err := json.Unmarshal(data, &event); err != nil {
			return fmt.Errorf("decode trade message: %w", err)
		}
		meta, ok := p.metaForRESTSymbol(event.Symbol)
		if !ok {
			return nil // Ignore unknown symbols
		}
		payload := schema.TradePayload{
			TradeID:   fmt.Sprintf("%s-%d", meta.canonical, event.TradeID),
			Side:      tradeSideFromAggressor(event.IsBuyerMaker),
			Price:     event.Price,
			Quantity:  event.Quantity,
			Timestamp: time.UnixMilli(event.TradeTime.Int64()).UTC(),
		}
		p.publisher.PublishTrade(p.ctx, meta.canonical, payload)
		if p.metrics != nil {
			p.metrics.recordEvent(p.ctx, telemetry.EventTypeTrade, meta.canonical)
		}
		return nil
	}

	// Ticker stream handler
	tickerHandler := func(data []byte) error {
		var event tickerMessage
		if err := json.Unmarshal(data, &event); err != nil {
			return fmt.Errorf("decode ticker message: %w", err)
		}
		meta, ok := p.metaForRESTSymbol(event.Symbol)
		if !ok {
			return nil // Ignore unknown symbols
		}
		payload := schema.TickerPayload{
			LastPrice: event.LastPrice,
			BidPrice:  event.BidPrice,
			AskPrice:  event.AskPrice,
			Volume24h: event.Volume,
			Timestamp: time.UnixMilli(event.EventTime.Int64()).UTC(),
		}
		p.publisher.PublishTicker(p.ctx, meta.canonical, payload)
		if p.metrics != nil {
			p.metrics.recordEvent(p.ctx, telemetry.EventTypeTicker, meta.canonical)
		}
		return nil
	}

	// Orderbook stream handler
	bookHandler := func(data []byte) error {
		var diff depthDiffMessage
		if err := json.Unmarshal(data, &diff); err != nil {
			return fmt.Errorf("decode depth message: %w", err)
		}
		meta, ok := p.metaForRESTSymbol(diff.Symbol)
		if !ok {
			return nil // Ignore unknown symbols
		}

		p.bookMu.Lock()
		handle, exists := p.bookHandles[meta.canonical]
		if !exists {
			handle = &bookHandle{
				assembler: shared.NewOrderBookAssembler(p.opts.SnapshotDepth),
				seqMu:     sync.Mutex{},
				lastSeq:   0,
				seeded:    atomic.Bool{},
				seeding:   atomic.Bool{},
				bufferMu:  sync.Mutex{},
				buffer:    nil,
			}
			p.bookHandles[meta.canonical] = handle
		}
		p.bookMu.Unlock()

		if !handle.seeded.Load() || handle.seeding.Load() {
			handle.appendDiff(diff)
			if !handle.seeding.Load() {
				go p.seedAndReplayBook(meta, handle)
			}
			return nil
		}

		if err := p.applyDepthDiff(meta, handle, diff); err != nil {
			if errors.Is(err, errOrderbookOutOfSync) {
				handle.seeded.Store(false)
				handle.requeueDiffs([]depthDiffMessage{diff})
				go p.seedAndReplayBook(meta, handle)
				return nil
			}
			return fmt.Errorf("apply depth diff %s: %w", meta.canonical, err)
		}
		return nil
	}

	// Create stream managers
	p.tradeManager = newStreamManager(ctx, baseURL, tradeHandler, p.errs, "trade", p.name)
	if err := p.tradeManager.start(); err != nil {
		return fmt.Errorf("start trade manager: %w", err)
	}

	p.tickerManager = newStreamManager(ctx, baseURL, tickerHandler, p.errs, "ticker", p.name)
	if err := p.tickerManager.start(); err != nil {
		return fmt.Errorf("start ticker manager: %w", err)
	}

	p.bookManager = newStreamManager(ctx, baseURL, bookHandler, p.errs, "orderbook", p.name)
	if err := p.bookManager.start(); err != nil {
		return fmt.Errorf("start book manager: %w", err)
	}

	return nil
}

func (p *Provider) hasTradingCredentials() bool {
	return strings.TrimSpace(p.opts.APIKey) != "" && strings.TrimSpace(p.opts.APISecret) != ""
}

func (p *Provider) startUserDataStream() {
	p.userStreamMu.Lock()
	defer p.userStreamMu.Unlock()
	if p.userStreamCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(p.ctx)
	p.userStreamCancel = cancel
	p.userStreamWG.Add(1)
	go func() {
		defer p.userStreamWG.Done()
		p.runUserDataStream(ctx)
	}()
}

func (p *Provider) stopUserDataStream() {
	p.userStreamMu.Lock()
	cancel := p.userStreamCancel
	p.userStreamCancel = nil
	p.userStreamMu.Unlock()
	if cancel != nil {
		cancel()
		p.userStreamWG.Wait()
	}
}

func (p *Provider) runUserDataStream(ctx context.Context) {
	backoffCfg := backoff.NewExponentialBackOff()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		listenKey, err := p.createListenKey(ctx)
		if err != nil {
			p.reportError(fmt.Errorf("binance listen key: %w", err))
			sleep := backoffCfg.NextBackOff()
			select {
			case <-ctx.Done():
				return
			case <-time.After(sleep):
			}
			continue
		}
		backoffCfg.Reset()
		if err := p.publishBalanceSnapshot(ctx); err != nil {
			p.reportError(fmt.Errorf("binance balance snapshot: %w", err))
		}
		err = p.consumeUserDataStream(ctx, listenKey)
		if errors.Is(err, context.Canceled) {
			return
		}
		if err != nil {
			p.reportError(fmt.Errorf("binance user stream: %w", err))
		}
		sleep := backoffCfg.NextBackOff()
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleep):
		}
	}
}

func (p *Provider) consumeUserDataStream(ctx context.Context, listenKey string) error {
	base := strings.TrimSuffix(p.opts.websocketURL(), "/")
	url := base + "/" + strings.TrimSpace(listenKey)
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("dial %s: %w", url, err)
	}
	defer func() {
		_ = conn.Close(websocket.StatusNormalClosure, "shutdown")
	}()
	keepCtx, keepCancel := context.WithCancel(ctx)
	defer keepCancel()
	interval := p.opts.userStreamKeepAliveDuration()
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-keepCtx.Done():
				return
			case <-ticker.C:
				if err := p.keepAliveListenKey(keepCtx, listenKey); err != nil {
					p.reportError(fmt.Errorf("binance listen key keepalive: %w", err))
				}
			}
		}
	}()
	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return context.Canceled
			}
			return fmt.Errorf("read %s: %w", url, err)
		}
		if msgType != websocket.MessageText {
			continue
		}
		p.handleUserDataMessage(data)
	}
}

func (p *Provider) handleUserDataMessage(data []byte) {
	var header userDataEvent
	if err := json.Unmarshal(data, &header); err != nil {
		p.reportError(fmt.Errorf("decode user data header: %w", err))
		return
	}
	switch strings.ToLower(header.EventType) {
	case "outboundaccountposition":
		var event accountPositionEvent
		if err := json.Unmarshal(data, &event); err != nil {
			p.reportError(fmt.Errorf("decode account position: %w", err))
			return
		}
		p.handleAccountPosition(event)
	case "balanceupdate":
		var event balanceDeltaEvent
		if err := json.Unmarshal(data, &event); err != nil {
			p.reportError(fmt.Errorf("decode balance update: %w", err))
			return
		}
		p.handleBalanceDelta(event)
	case "executionreport":
		var event executionReportEvent
		if err := json.Unmarshal(data, &event); err != nil {
			p.reportError(fmt.Errorf("decode execution report: %w", err))
			return
		}
		p.handleExecutionReport(event)
	default:
		// ignore other user data events for now
	}
}

func (p *Provider) handleAccountPosition(event accountPositionEvent) {
	timestamp := time.UnixMilli(event.EventTime.Int64()).UTC()
	for _, bal := range event.Balances {
		asset := strings.ToUpper(strings.TrimSpace(bal.Asset))
		if asset == "" {
			continue
		}
		free, _ := parseDecimal(bal.Free)
		locked, _ := parseDecimal(bal.Locked)
		p.publishBalance(asset, free, locked, timestamp)
	}
}

func (p *Provider) handleBalanceDelta(event balanceDeltaEvent) {
	asset := strings.ToUpper(strings.TrimSpace(event.Asset))
	if asset == "" {
		return
	}
	delta, ok := parseDecimal(event.Delta)
	if !ok {
		return
	}
	if delta.IsZero() {
		return
	}
	timestamp := time.UnixMilli(event.EventTime.Int64()).UTC()
	p.balanceMu.Lock()
	snapshot := p.balances[asset]
	snapshot.free = snapshot.free.Add(delta)
	p.balances[asset] = snapshot
	free := snapshot.free
	locked := snapshot.locked
	p.balanceMu.Unlock()
	p.publishBalance(asset, free, locked, timestamp)
}

func (p *Provider) handleExecutionReport(event executionReportEvent) {
	meta, ok := p.metaForRESTSymbol(event.Symbol)
	if !ok {
		return
	}
	side, err := binanceSideFromString(event.Side)
	if err != nil {
		p.reportError(fmt.Errorf("binance exec side: %w", err))
		return
	}
	orderType, err := binanceOrderTypeFromString(event.OrderType)
	if err != nil {
		p.reportError(fmt.Errorf("binance exec order type: %w", err))
		return
	}
	timestamp := time.UnixMilli(event.TransactionTime.Int64()).UTC()
	if timestamp.IsZero() {
		timestamp = time.UnixMilli(event.EventTime.Int64()).UTC()
	}
	clientOrderID := strings.TrimSpace(event.ClientOrderID)
	price := strings.TrimSpace(event.Price)
	if price == "" {
		price = strings.TrimSpace(event.LastExecutedPrice)
	}
	quantity := strings.TrimSpace(event.OriginalQuantity)
	filled := strings.TrimSpace(event.CumulativeQuantity)
	remaining := calculateRemaining(event.OriginalQuantity, event.CumulativeQuantity)
	avgFill := calculateAveragePrice(event.CumulativeQuoteQty, event.CumulativeQuantity)
	commissionAsset := ""
	if event.CommissionAsset != nil {
		commissionAsset = strings.TrimSpace(*event.CommissionAsset)
	}
	rejectReason := strings.TrimSpace(event.OrderRejectReason)
	payload := &schema.ExecReportPayload{
		ClientOrderID:    clientOrderID,
		ExchangeOrderID:  strconv.FormatInt(event.OrderID, 10),
		State:            binanceStatusToExecState(event.OrderStatus),
		Side:             side,
		OrderType:        orderType,
		Price:            price,
		Quantity:         quantity,
		FilledQuantity:   filled,
		RemainingQty:     remaining,
		AvgFillPrice:     avgFill,
		CommissionAmount: strings.TrimSpace(event.Commission),
		CommissionAsset:  commissionAsset,
		Timestamp:        timestamp,
		RejectReason:     nil,
	}
	if rejectReason != "" {
		localReason := rejectReason
		payload.RejectReason = &localReason
	}
	p.publisher.PublishExecReport(p.ctx, meta.canonical, *payload)
	if p.metrics != nil {
		tif := strings.ToUpper(strings.TrimSpace(event.TimeInForce))
		state := payload.State
		p.metrics.recordEvent(p.ctx, telemetry.EventTypeExecReport, meta.canonical)
		p.metrics.recordOrder(p.ctx, meta.canonical, side, orderType, tif, state)
		if state == schema.ExecReportStateREJECTED || state == schema.ExecReportStateEXPIRED {
			reason := rejectReason
			if reason == "" && payload.RejectReason != nil {
				reason = *payload.RejectReason
			}
			p.metrics.recordOrderRejection(p.ctx, meta.canonical, string(state), reason)
		}
		latency := time.Since(timestamp)
		if p.clock != nil {
			latency = p.clock().UTC().Sub(timestamp)
		}
		p.metrics.recordOrderLatency(p.ctx, meta.canonical, side, orderType, tif, state, latency)
	}
}

func (p *Provider) publishBalance(asset string, free, locked decimal.Decimal, timestamp time.Time) {
	total := free.Add(locked)
	snapshot := balanceSnapshot{free: free, locked: locked}
	p.balanceMu.Lock()
	p.balances[asset] = snapshot
	p.balanceMu.Unlock()
	payload := schema.BalanceUpdatePayload{
		Currency:  asset,
		Total:     total.String(),
		Available: free.String(),
		Timestamp: timestamp,
	}
	p.publisher.PublishBalanceUpdate(p.ctx, asset, payload)
	if p.metrics != nil {
		p.metrics.recordEvent(p.ctx, telemetry.EventTypeBalanceUpdate, asset)
		p.metrics.recordBalanceUpdate(p.ctx, asset)
	}
}

func (p *Provider) metaForRESTSymbol(symbol string) (symbolMeta, bool) {
	key := strings.ToUpper(strings.TrimSpace(symbol))
	if key == "" {
		var empty symbolMeta
		return empty, false
	}
	p.instrumentsMu.RLock()
	defer p.instrumentsMu.RUnlock()
	canonical, ok := p.restToCanon[key]
	if !ok {
		var empty symbolMeta
		return empty, false
	}
	meta, ok := p.symbols[canonical]
	return meta, ok
}

func (p *Provider) publishBalanceSnapshot(ctx context.Context) error {
	if !p.hasTradingCredentials() {
		return nil
	}
	balances, err := p.fetchAccountBalances(ctx)
	if err != nil {
		return err
	}
	timestamp := p.clock().UTC()
	for _, bal := range balances {
		asset := strings.ToUpper(strings.TrimSpace(bal.Asset))
		if asset == "" {
			continue
		}
		free, _ := parseDecimal(bal.Free)
		locked, _ := parseDecimal(bal.Locked)
		p.publishBalance(asset, free, locked, timestamp)
	}
	return nil
}

func (p *Provider) seedOrderBook(ctx context.Context, meta symbolMeta, handle *bookHandle) (schema.BookSnapshotPayload, error) {
	var empty schema.BookSnapshotPayload
	for {
		if ctx == nil {
			ctx = p.ctx
		}
		if ctx == nil {
			return empty, errors.New("binance: missing context for seeding orderbook")
		}

		snapshotCtx, cancel := context.WithTimeout(ctx, p.opts.httpTimeoutDuration())
		snapshot, err := p.fetchDepthSnapshot(snapshotCtx, meta.rest)
		cancel()
		if err != nil {
			return empty, err
		}
		if snapshot.LastUpdateID < 0 {
			continue
		}
		seq := uint64(snapshot.LastUpdateID)

		firstBuffered := handle.firstBufferedUpdateID()
		if firstBuffered != 0 && seq < firstBuffered {
			continue
		}

		now := time.Now().UTC()
		payload := &schema.BookSnapshotPayload{
			Bids:          levelsToPriceLevels(snapshot.Bids),
			Asks:          levelsToPriceLevels(snapshot.Asks),
			Checksum:      "",
			LastUpdate:    now,
			FirstUpdateID: seq,
			FinalUpdateID: seq,
		}

		if _, err := handle.assembler.ApplySnapshot(seq, *payload); err != nil {
			return empty, fmt.Errorf("apply snapshot: %w", err)
		}
		handle.seqMu.Lock()
		handle.lastSeq = seq
		handle.seqMu.Unlock()

		p.publisher.PublishBookSnapshot(p.ctx, meta.canonical, *payload)
		if p.metrics != nil {
			p.metrics.recordEvent(p.ctx, telemetry.EventTypeBookSnapshot, meta.canonical)
		}
		return *payload, nil
	}
}

func (p *Provider) seedAndReplayBook(meta symbolMeta, handle *bookHandle) {
	if handle == nil {
		return
	}
	if !handle.seeding.CompareAndSwap(false, true) {
		return
	}
	defer handle.seeding.Store(false)

	backoffCfg := backoff.NewExponentialBackOff()

outerLoop:
	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		snapshot, err := p.seedOrderBook(p.ctx, meta, handle)
		if err != nil {
			p.reportError(fmt.Errorf("seed orderbook %s: %w", meta.canonical, err))
			sleep := backoffCfg.NextBackOff()
			select {
			case <-p.ctx.Done():
				return
			case <-time.After(sleep):
				continue
			}
		}

		backoffCfg.Reset()

		currentSeq := snapshot.FinalUpdateID
		for {
			pending := handle.drainBuffered(currentSeq)
			if len(pending) == 0 {
				handle.seeded.Store(true)
				return
			}
			if len(pending) > 1 {
				sort.SliceStable(pending, func(i, j int) bool {
					return pending[i].FinalUpdateID < pending[j].FinalUpdateID
				})
			}

			for idx := range pending {
				if err := p.applyDepthDiff(meta, handle, pending[idx]); err != nil {
					handle.requeueDiffs(pending[idx:])
					if errors.Is(err, errOrderbookOutOfSync) {
						handle.seeded.Store(false)
					} else {
						p.reportError(fmt.Errorf("apply depth diff %s: %w", meta.canonical, err))
					}
					sleep := backoffCfg.NextBackOff()
					select {
					case <-p.ctx.Done():
						return
					case <-time.After(sleep):
					}
					continue outerLoop
				}
			}

			currentSeq = handle.currentSeq()
		}
	}
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
		Timestamp:  time.UnixMilli(diff.EventTime.Int64()).UTC(),
	}
	snapshot, applied, err := handle.assembler.ApplyDiff(update)
	if err != nil {
		return fmt.Errorf("apply diff: %w", err)
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
	if p.metrics != nil {
		p.metrics.recordEvent(p.ctx, telemetry.EventTypeBookSnapshot, meta.canonical)
	}
	return nil
}

func (p *Provider) submitOrder(ctx context.Context, meta symbolMeta, req schema.OrderRequest) error {
	params := url.Values{}
	params.Set("symbol", meta.rest)
	side, err := binanceSide(req.Side)
	if err != nil {
		return err
	}
	params.Set("side", side)
	typeValue, err := binanceOrderType(req.OrderType)
	if err != nil {
		return err
	}
	params.Set("type", typeValue)
	quantity := strings.TrimSpace(req.Quantity)
	if quantity == "" {
		return fmt.Errorf("binance: quantity required")
	}
	params.Set("quantity", quantity)
	limitPrice := ""
	if req.Price != nil {
		limitPrice = strings.TrimSpace(*req.Price)
	}
	if req.OrderType == schema.OrderTypeLimit {
		if limitPrice == "" {
			return fmt.Errorf("binance: limit order requires price")
		}
		params.Set("price", limitPrice)
		tifValue := strings.ToUpper(strings.TrimSpace(req.TIF))
		if tifValue == "" {
			tifValue = "GTC"
		}
		params.Set("timeInForce", tifValue)
	} else {
		if tif := strings.ToUpper(strings.TrimSpace(req.TIF)); tif != "" {
			params.Set("timeInForce", tif)
		}
	}
	if req.ClientOrderID != "" {
		params.Set("newClientOrderId", req.ClientOrderID)
	}
	params.Set("newOrderRespType", "FULL")
	if p.opts.recvWindowDuration() > 0 {
		params.Set("recvWindow", strconv.FormatInt(p.opts.recvWindowDuration().Milliseconds(), 10))
	}
	params.Set("timestamp", strconv.FormatInt(p.clock().UTC().UnixMilli(), 10))
	basePayload := params.Encode()
	signature := signPayload(basePayload, p.opts.APISecret)
	params.Set("signature", signature)
	body := params.Encode()
	endpoint := p.opts.orderEndpoint()
	if strings.TrimSpace(endpoint) == "" {
		return errors.New("binance: order endpoint not configured")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("create order request: %w", err)
	}
	httpReq.Header.Set("X-MBX-APIKEY", p.opts.APIKey)
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.httpClient().Do(httpReq)
	if err != nil {
		return fmt.Errorf("submit order: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read order response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return parseOrderError(resp.StatusCode, respBody)
	}
	var order orderResponse
	if err := json.Unmarshal(respBody, &order); err != nil {
		return fmt.Errorf("decode order response: %w", err)
	}
	p.publishOrderAcknowledgement(meta, req, order, quantity, limitPrice)
	return nil
}

func (p *Provider) publishOrderAcknowledgement(meta symbolMeta, req schema.OrderRequest, order orderResponse, fallbackQty, fallbackPrice string) {
	price := strings.TrimSpace(order.Price)
	if price == "" {
		price = strings.TrimSpace(fallbackPrice)
	}
	quantity := defaultIfEmpty(strings.TrimSpace(order.OrigQty), strings.TrimSpace(fallbackQty))
	filled := strings.TrimSpace(order.ExecutedQty)
	remaining := calculateRemaining(order.OrigQty, order.ExecutedQty)
	avgPrice := strings.TrimSpace(order.AvgPrice)
	if avgPrice == "" {
		cumQuote := defaultIfEmpty(strings.TrimSpace(order.CummulativeQuoteQty), strings.TrimSpace(order.CumQuote))
		avgPrice = calculateAveragePrice(cumQuote, order.ExecutedQty)
	}
	timestamp := resolveTimestamp(order.TransactTime, p.clock)
	if timestamp.IsZero() && order.UpdateTime > 0 {
		timestamp = resolveTimestamp(order.UpdateTime, p.clock)
	}
	clientOrderID := strings.TrimSpace(req.ClientOrderID)
	if clientOrderID == "" {
		clientOrderID = strings.TrimSpace(order.ClientOrderID)
	}
	payload := &schema.ExecReportPayload{
		ClientOrderID:    clientOrderID,
		ExchangeOrderID:  strconv.FormatInt(order.OrderID, 10),
		State:            binanceStatusToExecState(order.Status),
		Side:             req.Side,
		OrderType:        req.OrderType,
		Price:            price,
		Quantity:         quantity,
		FilledQuantity:   filled,
		RemainingQty:     remaining,
		AvgFillPrice:     avgPrice,
		CommissionAmount: "",
		CommissionAsset:  "",
		Timestamp:        timestamp,
		RejectReason:     nil,
	}
	p.publisher.PublishExecReport(p.ctx, meta.canonical, *payload)
	if p.metrics != nil {
		tif := strings.ToUpper(strings.TrimSpace(order.TimeInForce))
		state := payload.State
		p.metrics.recordEvent(p.ctx, telemetry.EventTypeExecReport, meta.canonical)
		p.metrics.recordOrder(p.ctx, meta.canonical, req.Side, req.OrderType, tif, state)
		if state == schema.ExecReportStateREJECTED || state == schema.ExecReportStateEXPIRED {
			p.metrics.recordOrderRejection(p.ctx, meta.canonical, string(state), strings.ToLower(string(state)))
		}
		latency := time.Since(timestamp)
		if p.clock != nil {
			latency = p.clock().UTC().Sub(timestamp)
		}
		p.metrics.recordOrderLatency(p.ctx, meta.canonical, req.Side, req.OrderType, tif, state, latency)
	}
}

func parseOrderError(status int, body []byte) error {
	var apiErr binanceError
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Msg != "" {
		if apiErr.Code != 0 {
			return fmt.Errorf("binance order error (%d): %s", apiErr.Code, apiErr.Msg)
		}
		return fmt.Errorf("binance order error: %s", apiErr.Msg)
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return fmt.Errorf("binance order error status %d", status)
	}
	return fmt.Errorf("binance order error status %d: %s", status, trimmed)
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
	if p.metrics != nil {
		op, result, disruption := classifyBinanceError(err)
		if op == "" {
			op = "adapter.binance"
		}
		if result == "" {
			result = "error"
		}
		p.metrics.recordVenueError(p.ctx, op, result)
		if disruption != "" {
			p.metrics.recordVenueDisruption(p.ctx, disruption)
		}
	}
}

func tradeSideFromAggressor(isBuyerMaker bool) schema.TradeSide {
	if isBuyerMaker {
		return schema.TradeSideSell
	}
	return schema.TradeSideBuy
}

type binanceTimestamp int64

func (ts *binanceTimestamp) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*ts = 0
		return nil
	}

	if trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
		inner := bytes.TrimSpace(trimmed[1 : len(trimmed)-1])
		if len(inner) == 0 {
			*ts = 0
			return nil
		}
		trimmed = inner
	}

	if len(trimmed) == 0 {
		*ts = 0
		return nil
	}

	if parsed, err := strconv.ParseInt(string(trimmed), 10, 64); err == nil {
		*ts = binanceTimestamp(parsed)
		return nil
	}

	if parsed, err := strconv.ParseFloat(string(trimmed), 64); err == nil {
		*ts = binanceTimestamp(int64(parsed))
		return nil
	}

	return fmt.Errorf("binance: invalid timestamp %q", string(data))
}

func (ts binanceTimestamp) Int64() int64 {
	return int64(ts)
}

type tradeMessage struct {
	EventType    string           `json:"e"`
	EventTime    binanceTimestamp `json:"E"`
	Symbol       string           `json:"s"`
	TradeID      int64            `json:"t"`
	Price        string           `json:"p"`
	Quantity     string           `json:"q"`
	TradeTime    binanceTimestamp `json:"T"`
	IsBuyerMaker bool             `json:"m"`
}

type tickerMessage struct {
	EventType string           `json:"e"`
	EventTime binanceTimestamp `json:"E"`
	Symbol    string           `json:"s"`
	LastPrice string           `json:"c"`
	BidPrice  string           `json:"b"`
	AskPrice  string           `json:"a"`
	Volume    string           `json:"v"`
}

type depthDiffMessage struct {
	EventType     string           `json:"e"`
	EventTime     binanceTimestamp `json:"E"`
	Symbol        string           `json:"s"`
	FirstUpdateID uint64           `json:"U"`
	FinalUpdateID uint64           `json:"u"`
	Bids          [][]string       `json:"b"`
	Asks          [][]string       `json:"a"`
}

type userDataEvent struct {
	EventType string           `json:"e"`
	EventTime binanceTimestamp `json:"E"`
}

type accountPositionEvent struct {
	EventType string                   `json:"e"`
	EventTime binanceTimestamp         `json:"E"`
	Balances  []accountPositionBalance `json:"B"`
}

type accountPositionBalance struct {
	Asset  string `json:"a"`
	Free   string `json:"f"`
	Locked string `json:"l"`
}

type balanceDeltaEvent struct {
	EventType string           `json:"e"`
	EventTime binanceTimestamp `json:"E"`
	Asset     string           `json:"a"`
	Delta     string           `json:"d"`
}

type executionReportEvent struct {
	EventType          string           `json:"e"`
	EventTime          binanceTimestamp `json:"E"`
	Symbol             string           `json:"s"`
	ClientOrderID      string           `json:"c"`
	Side               string           `json:"S"`
	OrderType          string           `json:"o"`
	TimeInForce        string           `json:"f"`
	OriginalQuantity   string           `json:"q"`
	Price              string           `json:"p"`
	StopPrice          string           `json:"P"`
	TrailingDelta      string           `json:"d"`
	OrderStatus        string           `json:"X"`
	OrderID            int64            `json:"i"`
	LastExecutedQty    string           `json:"l"`
	CumulativeQuantity string           `json:"z"`
	LastExecutedPrice  string           `json:"L"`
	Commission         string           `json:"n"`
	CommissionAsset    *string          `json:"N"`
	OrderRejectReason  string           `json:"r"`
	TransactionTime    binanceTimestamp `json:"T"`
	CumulativeQuoteQty string           `json:"Z"`
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

type orderResponse struct {
	Symbol              string `json:"symbol"`
	OrderID             int64  `json:"orderId"`
	ClientOrderID       string `json:"clientOrderId"`
	TransactTime        int64  `json:"transactTime"`
	UpdateTime          int64  `json:"updateTime"`
	Price               string `json:"price"`
	OrigQty             string `json:"origQty"`
	ExecutedQty         string `json:"executedQty"`
	CummulativeQuoteQty string `json:"cummulativeQuoteQty"`
	CumQuote            string `json:"cumQuote"`
	AvgPrice            string `json:"avgPrice"`
	Status              string `json:"status"`
	TimeInForce         string `json:"timeInForce"`
	Type                string `json:"type"`
	Side                string `json:"side"`
}

type binanceError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func signPayload(payload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func binanceSide(side schema.TradeSide) (string, error) {
	switch side {
	case schema.TradeSideBuy:
		return "BUY", nil
	case schema.TradeSideSell:
		return "SELL", nil
	default:
		return "", fmt.Errorf("binance: unsupported trade side %q", side)
	}
}

func binanceSideFromString(input string) (schema.TradeSide, error) {
	switch strings.ToUpper(strings.TrimSpace(input)) {
	case "BUY":
		return schema.TradeSideBuy, nil
	case "SELL":
		return schema.TradeSideSell, nil
	default:
		return schema.TradeSide(""), fmt.Errorf("binance: unsupported trade side %q", input)
	}
}

func binanceOrderType(orderType schema.OrderType) (string, error) {
	switch orderType {
	case schema.OrderTypeLimit:
		return "LIMIT", nil
	case schema.OrderTypeMarket:
		return "MARKET", nil
	default:
		return "", fmt.Errorf("binance: unsupported order type %q", orderType)
	}
}

func binanceOrderTypeFromString(input string) (schema.OrderType, error) {
	switch strings.ToUpper(strings.TrimSpace(input)) {
	case "LIMIT":
		return schema.OrderTypeLimit, nil
	case "MARKET":
		return schema.OrderTypeMarket, nil
	default:
		return schema.OrderType(""), fmt.Errorf("binance: unsupported order type %q", input)
	}
}

func binanceStatusToExecState(status string) schema.ExecReportState {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "NEW":
		return schema.ExecReportStateACK
	case "PARTIALLY_FILLED":
		return schema.ExecReportStatePARTIAL
	case "FILLED":
		return schema.ExecReportStateFILLED
	case "CANCELED":
		return schema.ExecReportStateCANCELLED
	case "REJECTED":
		return schema.ExecReportStateREJECTED
	case "EXPIRED":
		return schema.ExecReportStateEXPIRED
	default:
		return schema.ExecReportStateACK
	}
}

func calculateRemaining(orig, executed string) string {
	origDec, okOrig := parseDecimal(orig)
	execDec, okExec := parseDecimal(executed)
	if !okOrig || !okExec {
		return strings.TrimSpace(orig)
	}
	remaining := origDec.Sub(execDec)
	if remaining.Sign() < 0 {
		remaining = decimal.Zero
	}
	return remaining.String()
}

func calculateAveragePrice(quote, executed string) string {
	quoteDec, okQuote := parseDecimal(quote)
	execDec, okExec := parseDecimal(executed)
	if !okQuote || !okExec || execDec.Sign() == 0 {
		return ""
	}
	avg := quoteDec.Div(execDec)
	return avg.String()
}

func resolveTimestamp(ms int64, clock func() time.Time) time.Time {
	if ms > 0 {
		return time.UnixMilli(ms).UTC()
	}
	if clock != nil {
		return clock().UTC()
	}
	return time.Now().UTC()
}

func defaultIfEmpty(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return strings.TrimSpace(fallback)
}

func parseDecimal(value string) (decimal.Decimal, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return decimal.Zero, false
	}
	dec, err := decimal.NewFromString(trimmed)
	if err != nil {
		return decimal.Zero, false
	}
	return dec, true
}

type balanceSnapshot struct {
	free   decimal.Decimal
	locked decimal.Decimal
}

func (b balanceSnapshot) total() decimal.Decimal {
	return b.free.Add(b.locked)
}
