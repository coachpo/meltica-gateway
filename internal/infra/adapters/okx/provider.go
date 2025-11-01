package okx

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/app/dispatcher"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/adapters/shared"
	"github.com/coachpo/meltica/internal/infra/pool"
)

// Provider implements the OKX spot market adapter.
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
	metas         map[string]symbolMeta
	instIDToSym   map[string]string

	wsMu sync.Mutex
	ws   *wsManager

	tradeMu   sync.Mutex
	tradeSubs map[string]struct{}

	tickerMu   sync.Mutex
	tickerSubs map[string]struct{}

	bookMu      sync.Mutex
	bookSubs    map[string]struct{}
	bookHandles map[string]*bookHandle

	privateWsMu     sync.Mutex
	privateWs       *wsManager
	userStreamReady bool

	balanceMu sync.Mutex
	balances  map[string]balanceItem
}

type bookHandle struct {
	assembler *shared.OrderBookAssembler
	mu        sync.Mutex
	seeded    bool
}

// NewProvider constructs an OKX provider instance.
func NewProvider(opts Options) *Provider {
	opts = withDefaults(opts)
	p := &Provider{
		name:            opts.Config.Name,
		opts:            opts,
		pools:           opts.Pools,
		clock:           time.Now,
		client:          nil,
		events:          make(chan *schema.Event, 2048),
		errs:            make(chan error, 32),
		ctx:             nil,
		cancel:          nil,
		started:         atomic.Bool{},
		publisher:       nil,
		instrumentsMu:   sync.RWMutex{},
		instruments:     make(map[string]schema.Instrument),
		metas:           make(map[string]symbolMeta),
		instIDToSym:     make(map[string]string),
		wsMu:            sync.Mutex{},
		ws:              nil,
		tradeMu:         sync.Mutex{},
		tradeSubs:       make(map[string]struct{}),
		tickerMu:        sync.Mutex{},
		tickerSubs:      make(map[string]struct{}),
		bookMu:          sync.Mutex{},
		bookSubs:        make(map[string]struct{}),
		bookHandles:     make(map[string]*bookHandle),
		privateWsMu:     sync.Mutex{},
		privateWs:       nil,
		userStreamReady: false,
		balanceMu:       sync.Mutex{},
		balances:        make(map[string]balanceItem),
	}
	if p.pools == nil {
		log.Printf("okx/provider: Pools not injected; provider cannot start without shared PoolManager")
		panic("okx/provider: nil PoolManager in options")
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
		return errors.New("okx provider requires context")
	}
	if !p.started.CompareAndSwap(false, true) {
		return errors.New("okx provider already started")
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
		go p.startUserDataStream()
	}

	go func() {
		<-runCtx.Done()
		p.wsMu.Lock()
		if p.ws != nil {
			p.ws.stop()
			p.ws = nil
		}
		p.wsMu.Unlock()
		p.privateWsMu.Lock()
		if p.privateWs != nil {
			p.privateWs.stop()
			p.privateWs = nil
		}
		p.privateWsMu.Unlock()
		close(p.events)
		close(p.errs)
	}()

	return nil
}

// SubmitOrder proxies order submissions.
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
		return fmt.Errorf("okx: instrument %s not found", strings.TrimSpace(req.Symbol))
	}
	if !p.hasTradingCredentials() {
		return errors.New("okx: trading disabled (api credentials missing)")
	}
	if ctx == nil {
		ctx = p.ctx
	}
	return p.submitOrder(ctx, meta, req)
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

// Instruments returns the cached instrument catalogue.
func (p *Provider) Instruments() []schema.Instrument {
	p.instrumentsMu.RLock()
	defer p.instrumentsMu.RUnlock()
	out := make([]schema.Instrument, 0, len(p.instruments))
	for _, inst := range p.instruments {
		out = append(out, schema.CloneInstrument(inst))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Symbol < out[j].Symbol
	})
	return out
}

func (p *Provider) ensureRunning() error {
	if !p.started.Load() || p.ctx == nil {
		return errors.New("okx provider not started")
	}
	return nil
}

func (p *Provider) refreshInstruments(ctx context.Context) error {
	requestCtx, cancel := context.WithTimeout(ctx, p.opts.httpTimeoutDuration())
	defer cancel()
	list, metas, err := p.fetchInstruments(requestCtx)
	if err != nil {
		return err
	}
	p.instrumentsMu.Lock()
	p.instruments = make(map[string]schema.Instrument, len(list))
	p.metas = metas
	p.instIDToSym = make(map[string]string, len(metas))
	for _, inst := range list {
		cloned := schema.CloneInstrument(inst)
		p.instruments[inst.Symbol] = cloned
	}
	for symbol, meta := range metas {
		p.instIDToSym[strings.ToUpper(meta.instID)] = symbol
	}
	p.instrumentsMu.Unlock()
	p.publishInstrumentUpdates()
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
			}
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

func (p *Provider) configureTradeStreams(instruments []string) error {
	if len(instruments) == 0 {
		return nil
	}
	if err := p.ensureWS(); err != nil {
		return err
	}
	args := make([]wsArgument, 0)
	for _, symbol := range instruments {
		meta, ok := p.metaForInstrument(symbol)
		if !ok {
			continue
		}
		p.tradeMu.Lock()
		if _, exists := p.tradeSubs[meta.instID]; !exists {
			p.tradeSubs[meta.instID] = struct{}{}
			args = append(args, wsArgument{Channel: "trades", InstID: meta.instID})
		}
		p.tradeMu.Unlock()
	}
	if len(args) == 0 {
		return nil
	}
	return p.ws.subscribe(args)
}

func (p *Provider) unsubscribeTradeStreams(instruments []string) error {
	if len(instruments) == 0 {
		return nil
	}
	p.tradeMu.Lock()
	args := make([]wsArgument, 0)
	for _, symbol := range instruments {
		meta, ok := p.metaForInstrument(symbol)
		if !ok {
			continue
		}
		if _, exists := p.tradeSubs[meta.instID]; exists {
			delete(p.tradeSubs, meta.instID)
			args = append(args, wsArgument{Channel: "trades", InstID: meta.instID})
		}
	}
	p.tradeMu.Unlock()
	if len(args) == 0 {
		return nil
	}
	return p.ws.unsubscribe(args)
}

func (p *Provider) configureTickerStreams(instruments []string) error {
	if len(instruments) == 0 {
		return nil
	}
	if err := p.ensureWS(); err != nil {
		return err
	}
	args := make([]wsArgument, 0)
	for _, symbol := range instruments {
		meta, ok := p.metaForInstrument(symbol)
		if !ok {
			continue
		}
		p.tickerMu.Lock()
		if _, exists := p.tickerSubs[meta.instID]; !exists {
			p.tickerSubs[meta.instID] = struct{}{}
			args = append(args, wsArgument{Channel: "tickers", InstID: meta.instID})
		}
		p.tickerMu.Unlock()
	}
	if len(args) == 0 {
		return nil
	}
	return p.ws.subscribe(args)
}

func (p *Provider) unsubscribeTickerStreams(instruments []string) error {
	if len(instruments) == 0 {
		return nil
	}
	p.tickerMu.Lock()
	args := make([]wsArgument, 0)
	for _, symbol := range instruments {
		meta, ok := p.metaForInstrument(symbol)
		if !ok {
			continue
		}
		if _, exists := p.tickerSubs[meta.instID]; exists {
			delete(p.tickerSubs, meta.instID)
			args = append(args, wsArgument{Channel: "tickers", InstID: meta.instID})
		}
	}
	p.tickerMu.Unlock()
	if len(args) == 0 {
		return nil
	}
	return p.ws.unsubscribe(args)
}

func (p *Provider) configureOrderBookStreams(instruments []string) error {
	if len(instruments) == 0 {
		return nil
	}
	if err := p.ensureWS(); err != nil {
		return err
	}
	args := make([]wsArgument, 0)
	for _, symbol := range instruments {
		meta, ok := p.metaForInstrument(symbol)
		if !ok {
			continue
		}
		if err := p.ensureOrderBookHandle(symbol, meta); err != nil {
			return err
		}
		p.bookMu.Lock()
		if _, exists := p.bookSubs[meta.instID]; !exists {
			p.bookSubs[meta.instID] = struct{}{}
			args = append(args, wsArgument{Channel: "books", InstID: meta.instID})
		}
		p.bookMu.Unlock()
	}
	if len(args) == 0 {
		return nil
	}
	return p.ws.subscribe(args)
}

func (p *Provider) unsubscribeOrderBookStreams(instruments []string) error {
	if len(instruments) == 0 {
		return nil
	}
	p.bookMu.Lock()
	args := make([]wsArgument, 0)
	for _, symbol := range instruments {
		meta, ok := p.metaForInstrument(symbol)
		if !ok {
			continue
		}
		if _, exists := p.bookSubs[meta.instID]; exists {
			delete(p.bookSubs, meta.instID)
			args = append(args, wsArgument{Channel: "books", InstID: meta.instID})
		}
	}
	p.bookMu.Unlock()
	if len(args) == 0 {
		return nil
	}
	return p.ws.unsubscribe(args)
}

func (p *Provider) ensureOrderBookHandle(symbol string, meta symbolMeta) error {
	p.bookMu.Lock()
	handle, ok := p.bookHandles[symbol]
	if !ok {
		handle = &bookHandle{
			assembler: shared.NewOrderBookAssembler(p.opts.Config.SnapshotDepth),
			mu:        sync.Mutex{},
			seeded:    false,
		}
		p.bookHandles[symbol] = handle
	}
	p.bookMu.Unlock()

	handle.mu.Lock()
	defer handle.mu.Unlock()
	if handle.seeded {
		return nil
	}
	snapshot, seq, err := p.fetchOrderBookSnapshot(p.ctx, meta.instID)
	if err != nil {
		return fmt.Errorf("fetch okx snapshot: %w", err)
	}
	payload, err := handle.assembler.ApplySnapshot(seq, snapshot)
	if err != nil {
		return fmt.Errorf("apply okx snapshot: %w", err)
	}
	handle.seeded = true
	p.publisher.PublishBookSnapshot(p.ctx, symbol, payload)
	return nil
}

func (p *Provider) ensureWS() error {
	p.wsMu.Lock()
	defer p.wsMu.Unlock()
	if p.ws != nil {
		return nil
	}
	baseURL := p.opts.websocketURL()
	if strings.TrimSpace(baseURL) == "" {
		return errors.New("okx: websocket url not configured")
	}
	manager := newWSManager(p.ctx, baseURL, p.handleWSMessage, p.errs)
	if err := manager.start(); err != nil {
		return fmt.Errorf("start ws manager: %w", err)
	}
	p.ws = manager
	return nil
}

func (p *Provider) handleWSMessage(envelope wsEnvelope) error {
	channel := strings.ToLower(strings.TrimSpace(envelope.Arg.Channel))
	switch channel {
	case "trades":
		return p.handleTrades(envelope)
	case "tickers":
		return p.handleTickers(envelope)
	case "books":
		return p.handleBooks(envelope)
	default:
		return nil
	}
}

func (p *Provider) handleTrades(envelope wsEnvelope) error {
	for _, raw := range envelope.Data {
		var evt tradeEvent
		if err := json.Unmarshal(raw, &evt); err != nil {
			return fmt.Errorf("decode trade event: %w", err)
		}
		symbol, ok := p.symbolForInstID(evt.InstID)
		if !ok {
			continue
		}
		side := schema.TradeSideBuy
		switch strings.ToLower(strings.TrimSpace(evt.Side)) {
		case "sell":
			side = schema.TradeSideSell
		case "buy":
			side = schema.TradeSideBuy
		default:
		}
		payload := schema.TradePayload{
			TradeID:   strings.TrimSpace(evt.TradeID),
			Side:      side,
			Price:     strings.TrimSpace(evt.Price),
			Quantity:  strings.TrimSpace(evt.Quantity),
			Timestamp: parseMilliTimestamp(evt.Timestamp),
		}
		p.publisher.PublishTrade(p.ctx, symbol, payload)
	}
	return nil
}

func (p *Provider) handleTickers(envelope wsEnvelope) error {
	for _, raw := range envelope.Data {
		var evt tickerEvent
		if err := json.Unmarshal(raw, &evt); err != nil {
			return fmt.Errorf("decode ticker event: %w", err)
		}
		symbol, ok := p.symbolForInstID(evt.InstID)
		if !ok {
			continue
		}
		payload := schema.TickerPayload{
			LastPrice: strings.TrimSpace(evt.Last),
			BidPrice:  strings.TrimSpace(evt.Bid),
			AskPrice:  strings.TrimSpace(evt.Ask),
			Volume24h: strings.TrimSpace(evt.Volume24h),
			Timestamp: parseMilliTimestamp(evt.Timestamp),
		}
		p.publisher.PublishTicker(p.ctx, symbol, payload)
	}
	return nil
}

func (p *Provider) handleBooks(envelope wsEnvelope) error {
	for _, raw := range envelope.Data {
		var evt bookEvent
		if err := json.Unmarshal(raw, &evt); err != nil {
			return fmt.Errorf("decode book event: %w", err)
		}
		symbol, ok := p.symbolForInstID(evt.InstID)
		if !ok {
			continue
		}
		p.bookMu.Lock()
		handle := p.bookHandles[symbol]
		p.bookMu.Unlock()
		if handle == nil {
			continue
		}
		seq := evt.SequenceID()
		if seq == 0 {
			continue
		}
		ts := parseMilliTimestamp(evt.Timestamp)
		if strings.EqualFold(strings.TrimSpace(evt.Action), "snapshot") {
			snapshot := schema.BookSnapshotPayload{
				Bids:          convertPriceLevels(evt.Bids),
				Asks:          convertPriceLevels(evt.Asks),
				Checksum:      strconv.Itoa(int(evt.Checksum)),
				LastUpdate:    ts,
				FirstUpdateID: seq,
				FinalUpdateID: seq,
			}
			handle.mu.Lock()
			payload, err := handle.assembler.ApplySnapshot(seq, snapshot)
			handle.seeded = true
			handle.mu.Unlock()
			if err != nil {
				p.reportError(fmt.Errorf("apply okx snapshot: %w", err))
				continue
			}
			p.publisher.PublishBookSnapshot(p.ctx, symbol, payload)
			continue
		}
		diff := shared.OrderBookDiff{
			SequenceID: seq,
			Bids:       convertDiffLevels(evt.Bids),
			Asks:       convertDiffLevels(evt.Asks),
			Timestamp:  ts,
		}
		handle.mu.Lock()
		payload, applied, err := handle.assembler.ApplyDiff(diff)
		handle.mu.Unlock()
		if err != nil {
			p.reportError(fmt.Errorf("apply okx diff: %w", err))
			continue
		}
		if !applied {
			continue
		}
		p.publisher.PublishBookSnapshot(p.ctx, symbol, payload)
	}
	return nil
}

func (p *Provider) metaForInstrument(symbol string) (symbolMeta, bool) {
	p.instrumentsMu.RLock()
	defer p.instrumentsMu.RUnlock()
	meta, ok := p.metas[strings.ToUpper(strings.TrimSpace(symbol))]
	return meta, ok
}

func (p *Provider) symbolForInstID(instID string) (string, bool) {
	p.instrumentsMu.RLock()
	defer p.instrumentsMu.RUnlock()
	symbol, ok := p.instIDToSym[strings.ToUpper(strings.TrimSpace(instID))]
	return symbol, ok
}

func convertDiffLevels(levels [][]string) []shared.DiffLevel {
	if len(levels) == 0 {
		return nil
	}
	out := make([]shared.DiffLevel, 0, len(levels))
	for _, level := range levels {
		if len(level) < 2 {
			continue
		}
		price := strings.TrimSpace(level[0])
		qty := strings.TrimSpace(level[1])
		if price == "" {
			continue
		}
		out = append(out, shared.DiffLevel{Price: price, Quantity: qty})
	}
	return out
}

type tradeEvent struct {
	InstID    string `json:"instId"`
	TradeID   string `json:"tradeId"`
	Price     string `json:"px"`
	Quantity  string `json:"sz"`
	Side      string `json:"side"`
	Timestamp string `json:"ts"`
}

type tickerEvent struct {
	InstID    string `json:"instId"`
	Last      string `json:"last"`
	Bid       string `json:"bidPx"`
	Ask       string `json:"askPx"`
	Volume24h string `json:"vol24h"`
	Timestamp string `json:"ts"`
}

type bookEvent struct {
	InstID    string      `json:"instId"`
	Asks      [][]string  `json:"asks"`
	Bids      [][]string  `json:"bids"`
	SeqID     json.Number `json:"seqId"`
	PrevSeqID json.Number `json:"prevSeqId"`
	Checksum  int32       `json:"checksum"`
	Timestamp string      `json:"ts"`
	Action    string      `json:"action"`
}

func (b bookEvent) SequenceID() uint64 {
	seqStr := b.SeqID.String()
	if seqStr == "" {
		return 0
	}
	seq, err := strconv.ParseUint(seqStr, 10, 64)
	if err != nil {
		return 0
	}
	return seq
}

func extractInstruments(filters []dispatcher.FilterRule, provider string) []string {
	global := make(map[string]struct{})
	specific := make(map[string]struct{})
	providerField := providerInstrumentField(provider)
	addValues := func(target map[string]struct{}, value any) {
		switch v := value.(type) {
		case nil:
			return
		case string:
			trimmed := strings.TrimSpace(strings.ToUpper(v))
			if trimmed != "" {
				target[trimmed] = struct{}{}
			}
		case []string:
			for _, entry := range v {
				trimmed := strings.TrimSpace(strings.ToUpper(entry))
				if trimmed != "" {
					target[trimmed] = struct{}{}
				}
			}
		case []any:
			for _, entry := range v {
				trimmed := strings.TrimSpace(strings.ToUpper(fmt.Sprint(entry)))
				if trimmed != "" {
					target[trimmed] = struct{}{}
				}
			}
		default:
			trimmed := strings.TrimSpace(strings.ToUpper(fmt.Sprint(v)))
			if trimmed != "" {
				target[trimmed] = struct{}{}
			}
		}
	}

	for _, filter := range filters {
		field := strings.TrimSpace(strings.ToLower(filter.Field))
		if field == "instrument" {
			addValues(global, filter.Value)
		}
		if field == strings.ToLower(providerField) {
			addValues(specific, filter.Value)
		}
	}
	if len(specific) > 0 {
		return setToSortedSlice(specific)
	}
	if len(global) > 0 {
		return setToSortedSlice(global)
	}
	return nil
}

func providerInstrumentField(provider string) string {
	trimmed := strings.TrimSpace(provider)
	if trimmed == "" {
		return "instrument"
	}
	return "instrument@" + strings.ToLower(trimmed)
}

func setToSortedSlice(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func (p *Provider) hasTradingCredentials() bool {
	return strings.TrimSpace(p.opts.Config.APIKey) != "" &&
		strings.TrimSpace(p.opts.Config.APISecret) != "" &&
		strings.TrimSpace(p.opts.Config.Passphrase) != ""
}

func (p *Provider) startUserDataStream() {
	if err := p.ensurePrivateWS(); err != nil {
		p.reportError(fmt.Errorf("okx: failed to start private ws: %w", err))
		return
	}

	if err := p.publishBalanceSnapshot(p.ctx); err != nil {
		p.reportError(fmt.Errorf("okx: balance snapshot: %w", err))
	}

	loginArgs := []wsArgument{{Channel: "login", InstID: ""}}
	if err := p.privateWs.subscribe(loginArgs); err != nil {
		p.reportError(fmt.Errorf("okx: login failed: %w", err))
		return
	}

	time.Sleep(500 * time.Millisecond)

	subscriptions := []wsArgument{
		{Channel: "orders", InstID: ""},
		{Channel: "account", InstID: ""},
	}

	if err := p.privateWs.subscribe(subscriptions); err != nil {
		p.reportError(fmt.Errorf("okx: subscribe to user streams: %w", err))
	}
}

func (p *Provider) ensurePrivateWS() error {
	p.privateWsMu.Lock()
	defer p.privateWsMu.Unlock()

	if p.privateWs != nil {
		return nil
	}

	baseURL := p.opts.privateWebsocketURL()
	if strings.TrimSpace(baseURL) == "" {
		return errors.New("okx: private websocket url not configured")
	}

	manager := newWSManager(p.ctx, baseURL, p.handlePrivateWSMessage, p.errs)
	manager.setAuthFunc(p.generateLoginRequest)

	if err := manager.start(); err != nil {
		return fmt.Errorf("start private ws manager: %w", err)
	}

	p.privateWs = manager
	return nil
}

func (p *Provider) generateLoginRequest() *wsRequest {
	timestamp := strconv.FormatInt(p.clock().UTC().Unix(), 10)
	message := timestamp + "GET" + "/users/self/verify"

	mac := hmac.New(sha256.New, []byte(p.opts.Config.APISecret))
	mac.Write([]byte(message))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	loginReq := wsLoginRequest{
		Op: "login",
		Args: []wsLoginArg{
			{
				APIKey:     p.opts.Config.APIKey,
				Passphrase: p.opts.Config.Passphrase,
				Timestamp:  timestamp,
				Sign:       signature,
			},
		},
	}

	data, _ := json.Marshal(loginReq)
	return &wsRequest{
		ID:   "",
		Op:   "login",
		Args: []wsArgument{{Channel: string(data), InstID: ""}},
	}
}

type wsLoginRequest struct {
	Op   string       `json:"op"`
	Args []wsLoginArg `json:"args"`
}

type wsLoginArg struct {
	APIKey     string `json:"apiKey"`
	Passphrase string `json:"passphrase"`
	Timestamp  string `json:"timestamp"`
	Sign       string `json:"sign"`
}

func (p *Provider) handlePrivateWSMessage(envelope wsEnvelope) error {
	channel := strings.ToLower(strings.TrimSpace(envelope.Arg.Channel))

	switch channel {
	case "orders":
		return p.handleOrders(envelope)
	case "account":
		return p.handleAccount(envelope)
	case "login":
		p.privateWsMu.Lock()
		p.userStreamReady = true
		p.privateWsMu.Unlock()
		return nil
	default:
		return nil
	}
}

func (p *Provider) handleOrders(envelope wsEnvelope) error {
	for _, raw := range envelope.Data {
		var order orderUpdateEvent
		if err := json.Unmarshal(raw, &order); err != nil {
			return fmt.Errorf("decode order event: %w", err)
		}

		symbol, ok := p.symbolForInstID(order.InstID)
		if !ok {
			continue
		}

		side := schema.TradeSideBuy
		if strings.EqualFold(strings.TrimSpace(order.Side), "sell") {
			side = schema.TradeSideSell
		}

		state := mapOrderState(order.State)

		payload := schema.ExecReportPayload{
			ExchangeOrderID:  strings.TrimSpace(order.OrdID),
			ClientOrderID:    strings.TrimSpace(order.ClOrdID),
			State:            state,
			Side:             side,
			OrderType:        mapOrderType(order.OrdType),
			Price:            strings.TrimSpace(order.Px),
			Quantity:         strings.TrimSpace(order.Sz),
			FilledQuantity:   strings.TrimSpace(order.AccFillSz),
			RemainingQty:     "",
			AvgFillPrice:     strings.TrimSpace(order.AvgPx),
			CommissionAmount: "",
			CommissionAsset:  "",
			Timestamp:        parseMilliTimestamp(order.UTime),
			RejectReason:     nil,
		}

		p.publisher.PublishExecReport(p.ctx, symbol, payload)
	}
	return nil
}

func (p *Provider) handleAccount(envelope wsEnvelope) error {
	for _, raw := range envelope.Data {
		var acct accountUpdateEvent
		if err := json.Unmarshal(raw, &acct); err != nil {
			return fmt.Errorf("decode account event: %w", err)
		}

		for _, detail := range acct.Details {
			p.balanceMu.Lock()
			p.balances[strings.ToUpper(strings.TrimSpace(detail.Ccy))] = balanceItem{
				Ccy:       strings.ToUpper(strings.TrimSpace(detail.Ccy)),
				AvailBal:  strings.TrimSpace(detail.AvailBal),
				CashBal:   strings.TrimSpace(detail.CashBal),
				FrozenBal: strings.TrimSpace(detail.FrozenBal),
			}
			p.balanceMu.Unlock()

			payload := schema.BalanceUpdatePayload{
				Currency:  strings.ToUpper(strings.TrimSpace(detail.Ccy)),
				Available: strings.TrimSpace(detail.AvailBal),
				Total:     strings.TrimSpace(detail.CashBal),
				Timestamp: p.clock(),
			}

			p.publisher.PublishBalanceUpdate(p.ctx, payload.Currency, payload)
		}
	}
	return nil
}

func (p *Provider) publishBalanceSnapshot(ctx context.Context) error {
	balances, err := p.fetchAccountBalances(ctx)
	if err != nil {
		return err
	}

	p.balanceMu.Lock()
	for _, bal := range balances {
		currency := strings.ToUpper(strings.TrimSpace(bal.Ccy))
		p.balances[currency] = bal

		payload := schema.BalanceUpdatePayload{
			Currency:  currency,
			Available: strings.TrimSpace(bal.AvailBal),
			Total:     strings.TrimSpace(bal.CashBal),
			Timestamp: p.clock(),
		}

		p.publisher.PublishBalanceUpdate(p.ctx, currency, payload)
	}
	p.balanceMu.Unlock()

	return nil
}

type orderUpdateEvent struct {
	InstID    string `json:"instId"`
	OrdID     string `json:"ordId"`
	ClOrdID   string `json:"clOrdId"`
	Px        string `json:"px"`
	Sz        string `json:"sz"`
	OrdType   string `json:"ordType"`
	Side      string `json:"side"`
	State     string `json:"state"`
	AccFillSz string `json:"accFillSz"`
	AvgPx     string `json:"avgPx"`
	UTime     string `json:"uTime"`
}

type accountUpdateEvent struct {
	UTime   string                `json:"uTime"`
	Details []accountDetailUpdate `json:"details"`
}

type accountDetailUpdate struct {
	Ccy       string `json:"ccy"`
	AvailBal  string `json:"availBal"`
	CashBal   string `json:"cashBal"`
	FrozenBal string `json:"frozenBal"`
}

func mapOrderState(state string) schema.ExecReportState {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "live":
		return schema.ExecReportStateACK
	case "partially_filled":
		return schema.ExecReportStatePARTIAL
	case "filled":
		return schema.ExecReportStateFILLED
	case "canceled":
		return schema.ExecReportStateCANCELLED
	default:
		return schema.ExecReportStateACK
	}
}

func mapOrderType(ordType string) schema.OrderType {
	switch strings.ToLower(strings.TrimSpace(ordType)) {
	case "market":
		return schema.OrderTypeMarket
	case "limit":
		return schema.OrderTypeLimit
	default:
		return schema.OrderTypeLimit
	}
}
