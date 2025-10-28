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
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/coder/websocket"
	json "github.com/goccy/go-json"
	"github.com/shopspring/decimal"

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

	userStreamMu     sync.Mutex
	userStreamCancel context.CancelFunc
	userStreamWG     sync.WaitGroup

	balanceMu sync.Mutex
	balances  map[string]balanceSnapshot
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
	p.balances = make(map[string]balanceSnapshot)
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
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	p.instrumentsMu.RLock()
	defer p.instrumentsMu.RUnlock()
	meta, ok := p.symbols[normalized]
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
	p.stopUserDataStream()
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
	base := strings.TrimSuffix(p.opts.WebsocketBaseURL, "/")
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
	interval := p.opts.UserStreamKeepAlive
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
	payload := schema.ExecReportPayload{
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
		Timestamp:        timestamp,
		CommissionAmount: strings.TrimSpace(event.Commission),
		CommissionAsset:  commissionAsset,
	}
	p.publisher.PublishExecReport(p.ctx, meta.canonical, payload)
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
}

func (p *Provider) metaForRESTSymbol(symbol string) (symbolMeta, bool) {
	key := strings.ToUpper(strings.TrimSpace(symbol))
	if key == "" {
		return symbolMeta{}, false
	}
	p.instrumentsMu.RLock()
	defer p.instrumentsMu.RUnlock()
	canonical, ok := p.restToCanon[key]
	if !ok {
		return symbolMeta{}, false
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
			Timestamp: time.UnixMilli(event.TradeTime.Int64()).UTC(),
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
			Timestamp: time.UnixMilli(event.EventTime.Int64()).UTC(),
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
		Timestamp:  time.UnixMilli(diff.EventTime.Int64()).UTC(),
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
	if p.opts.RecvWindow > 0 {
		params.Set("recvWindow", strconv.FormatInt(p.opts.RecvWindow.Milliseconds(), 10))
	}
	params.Set("timestamp", strconv.FormatInt(p.clock().UTC().UnixMilli(), 10))
	basePayload := params.Encode()
	signature := signPayload(basePayload, p.opts.APISecret)
	params.Set("signature", signature)
	body := params.Encode()
	endpoint := strings.TrimSuffix(p.opts.APIBaseURL, "/") + "/api/v3/order"
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
	defer resp.Body.Close()
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
	avgPrice := calculateAveragePrice(order.CummulativeQuoteQty, order.ExecutedQty)
	timestamp := resolveTimestamp(order.TransactTime, p.clock)
	clientOrderID := strings.TrimSpace(req.ClientOrderID)
	if clientOrderID == "" {
		clientOrderID = strings.TrimSpace(order.ClientOrderID)
	}
	payload := schema.ExecReportPayload{
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
		Timestamp:        timestamp,
		CommissionAmount: "",
		CommissionAsset:  "",
	}
	p.publisher.PublishExecReport(p.ctx, meta.canonical, payload)
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
	Price               string `json:"price"`
	OrigQty             string `json:"origQty"`
	ExecutedQty         string `json:"executedQty"`
	CummulativeQuoteQty string `json:"cummulativeQuoteQty"`
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
