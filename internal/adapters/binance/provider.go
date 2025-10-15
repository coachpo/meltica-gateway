package binance

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
	"github.com/sourcegraph/conc"
)

// ProviderOptions configure the Binance provider runtime.
type ProviderOptions struct {
	Topics    []string
	Snapshots []RESTPoller
	Pools     *pool.PoolManager
}

// Provider streams canonical events from Binance transports.
type Provider struct {
	name  string
	ws    *WSClient
	rest  *RESTClient
	opts  ProviderOptions
	clock func() time.Time

	events chan *schema.Event
	errs   chan error
	orders chan schema.OrderRequest

	started atomic.Bool

	mu        sync.Mutex
	routes    map[string]*routeHandle
	ctx       context.Context
	cancel    context.CancelFunc
	orderMu   sync.RWMutex
	orderBook map[string]schema.OrderRequest
	reports   map[string]schema.ExecReport
	bookMu    sync.Mutex
	books     map[string]*BookAssembler
	pending   map[string][]*schema.Event
	pools     *pool.PoolManager
}

type routeHandle struct {
	cancel context.CancelFunc
	wg     conc.WaitGroup
}

// NewProvider constructs a Binance market data provider.
func NewProvider(name string, ws *WSClient, rest *RESTClient, opts ProviderOptions) *Provider {
	if name == "" {
		name = "binance"
	}
	provider := new(Provider)
	provider.name = name
	provider.ws = ws
	provider.rest = rest
	provider.opts = opts
	provider.clock = time.Now

	// Note: Book assembler is passed to ws client in constructor
	// Type assertion not needed as ws is already of correct type

	provider.events = make(chan *schema.Event, 128)
	provider.errs = make(chan error, 8)
	provider.orders = make(chan schema.OrderRequest, 128)
	provider.routes = make(map[string]*routeHandle)
	provider.orderBook = make(map[string]schema.OrderRequest)
	provider.reports = make(map[string]schema.ExecReport)
	provider.books = make(map[string]*BookAssembler)
	provider.pending = make(map[string][]*schema.Event)
	provider.pools = opts.Pools
	return provider
}

// Start begins streaming events until the context is cancelled.
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
		close(p.events)
		close(p.errs)
		close(p.orders)
	}()

	go p.consumeOrders(ctx)

	if len(p.opts.Topics) > 0 || len(p.opts.Snapshots) > 0 {
		p.mu.Lock()
		p.routes["__bootstrap__"] = p.startRouteLocked("__bootstrap__", p.opts.Topics, p.opts.Snapshots)
		p.mu.Unlock()
	}

	return nil
}

// Events returns the canonical event stream produced by the provider.
func (p *Provider) Events() <-chan *schema.Event {
	return p.events
}

// Errors returns asynchronous provider errors.
func (p *Provider) Errors() <-chan error {
	return p.errs
}

// SubmitOrder enqueues an order for asynchronous processing.
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
		return fmt.Errorf("provider shutting down")
	case p.orders <- req:
		return nil
	}
}

// SubscribeRoute activates streaming for the specified dispatcher route.
func (p *Provider) SubscribeRoute(route dispatcher.Route) error {
	if route.Type == "" {
		return errors.New("route type required")
	}
	if p.ctx == nil {
		return errors.New("provider not started")
	}
	key := string(route.Type)
	pollers := pollersFromRoute(route.RestFns)

	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.routes[key]; exists {
		return nil
	}
	handle := p.startRouteLocked(key, route.WSTopics, pollers)
	p.routes[key] = handle
	return nil
}

// UnsubscribeRoute stops streaming for the provided canonical type.
func (p *Provider) UnsubscribeRoute(typ schema.CanonicalType) error {
	if typ == "" {
		return errors.New("route type required")
	}
	key := string(typ)

	p.mu.Lock()
	handle, ok := p.routes[key]
	if ok {
		delete(p.routes, key)
	}
	p.mu.Unlock()

	if ok && handle != nil {
		handle.cancel()
		handle.wg.Wait()
	}
	return nil
}

func (p *Provider) stopAll() {
	p.mu.Lock()
	handles := make([]*routeHandle, 0, len(p.routes))
	for key, handle := range p.routes {
		handles = append(handles, handle)
		delete(p.routes, key)
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

func (p *Provider) consumeOrders(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.ctx.Done():
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
	key := orderKey(order.Provider, order.ClientOrderID)
	p.orderMu.Lock()
	p.orderBook[key] = order
	p.orderMu.Unlock()

	report := schema.ExecReport{
		ClientOrderID: order.ClientOrderID,
		Provider:      order.Provider,
		Symbol:        order.Symbol,
		Status:        schema.ExecReportStateACK,
		TransactTime:  time.Now().UTC().UnixNano(),
		TraceID:       order.ClientOrderID,
		DecisionID:    order.ConsumerID,
	}
	p.orderMu.Lock()
	p.reports[key] = report
	p.orderMu.Unlock()

	evt := p.borrowEvent(p.ctx)
	if evt == nil {
		return
	}
	now := p.clock().UTC()
	evt.EventID = fmt.Sprintf("execreport:%s", order.ClientOrderID)
	evt.Provider = order.Provider
	evt.Symbol = order.Symbol
	evt.Type = schema.EventTypeExecReport
	evt.SeqProvider = uint64(now.UnixNano())
	evt.IngestTS = now
	evt.EmitTS = now
	evt.Payload = schema.ExecReportPayload{
		ClientOrderID:   order.ClientOrderID,
		ExchangeOrderID: order.ClientOrderID,
		State:           schema.ExecReportStateACK,
		Side:            order.Side,
		OrderType:       order.OrderType,
		Price:           valueOrDefault(order.Price),
		Quantity:        order.Quantity,
		FilledQuantity:  "0",
		RemainingQty:    order.Quantity,
		AvgFillPrice:    "0",
		Timestamp:       time.Now().UTC(),
	}
	p.emitEvent(evt)
}

func (p *Provider) borrowEvent(ctx context.Context) *schema.Event {
	requestCtx := ctx
	if requestCtx == nil {
		requestCtx = p.ctx
	}
	if requestCtx == nil {
		requestCtx = context.Background()
	}
	evt, err := p.pools.BorrowCanonicalEvent(requestCtx)
	if err != nil {
		log.Printf("binance provider %s: borrow canonical event failed: %v", p.name, err)
		p.emitError(fmt.Errorf("borrow canonical event: %w", err))
		return nil
	}
	return evt
}

// QueryOrder returns the latest execution report for the provided identifiers.
func (p *Provider) QueryOrder(_ context.Context, provider, clientOrderID string) (schema.ExecReport, bool, error) {
	if provider = strings.TrimSpace(provider); provider == "" {
		provider = p.name
	}
	key := orderKey(provider, clientOrderID)
	p.orderMu.RLock()
	report, ok := p.reports[key]
	p.orderMu.RUnlock()
	if !ok {
		var empty schema.ExecReport
		return empty, false, nil
	}
	return report, true, nil
}

func (p *Provider) startRouteLocked(_ string, topics []string, pollers []RESTPoller) *routeHandle {
	hasStreams := (len(topics) > 0 && p.ws != nil) || (len(pollers) > 0 && p.rest != nil)
	if !hasStreams {
		emptyHandle := new(routeHandle)
		emptyHandle.cancel = func() {}
		return emptyHandle
	}

	routeCtx, cancel := context.WithCancel(p.ctx)
	handle := new(routeHandle)
	handle.cancel = cancel

	if len(topics) > 0 && p.ws != nil {
		events, errs := p.ws.Stream(routeCtx, topics)
		handle.wg.Go(func() {
			p.pipeEvents(routeCtx, events)
		})
		if errs != nil {
			handle.wg.Go(func() {
				p.pipeErrors(routeCtx, errs)
			})
		}
	}

	if len(pollers) > 0 && p.rest != nil {
		events, errs := p.rest.Poll(routeCtx, pollers)
		handle.wg.Go(func() {
			p.pipeEvents(routeCtx, events)
		})
		if errs != nil {
			handle.wg.Go(func() {
				p.pipeErrors(routeCtx, errs)
			})
		}
	}

	return handle
}

func (p *Provider) pipeEvents(ctx context.Context, stream <-chan *schema.Event) {
	if stream == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-stream:
			if !ok {
				return
			}
			p.emitEvent(evt)
		}
	}
}

func (p *Provider) pipeErrors(ctx context.Context, errs <-chan error) {
	if errs == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-errs:
			if !ok {
				return
			}
			p.emitError(err)
		}
	}
}

func (p *Provider) bookAssembler(symbol string) *BookAssembler {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return nil
	}
	p.bookMu.Lock()
	defer p.bookMu.Unlock()
	assembler, ok := p.books[symbol]
	if !ok {
		assembler = NewBookAssembler()
		p.books[symbol] = assembler
	}
	return assembler
}

func (p *Provider) prepareOrderBook(evt *schema.Event) ([]*schema.Event, bool, error) {
	if evt == nil {
		return nil, false, nil
	}
	switch evt.Type {
	case schema.EventTypeBookSnapshot:
		payload, ok := coerceBookSnapshot(evt.Payload)
		if !ok {
			return nil, false, fmt.Errorf("provider %s: invalid book snapshot payload", p.name)
		}
		assembler := p.bookAssembler(evt.Symbol)
		if assembler == nil {
			return nil, true, nil
		}
		snap, err := assembler.ApplySnapshot(payload, evt.SeqProvider)
		if err != nil {
			return nil, false, err
		}
		evt.Payload = snap
		extra := p.flushBufferedUpdates(evt.Symbol, assembler)
		return extra, true, nil
	case schema.EventTypeBookUpdate:
		payload, ok := coerceBookUpdate(evt.Payload)
		if !ok {
			return nil, false, fmt.Errorf("provider %s: invalid book update payload", p.name)
		}
		assembler := p.bookAssembler(evt.Symbol)
		if assembler == nil {
			return nil, true, nil
		}
		update, err := assembler.ApplyUpdate(payload, evt.SeqProvider)
		if err != nil {
			if errors.Is(err, ErrBookNotInitialized) {
				p.bufferBookUpdate(evt.Symbol, evt)
				return nil, false, nil
			}
			return nil, false, err
		}
		evt.Payload = update
		return nil, true, nil
	default:
		return nil, true, nil
	}
}

func (p *Provider) bufferBookUpdate(symbol string, evt *schema.Event) {
	if evt == nil {
		return
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return
	}
	p.bookMu.Lock()
	p.pending[symbol] = append(p.pending[symbol], evt)
	p.bookMu.Unlock()
}

func (p *Provider) flushBufferedUpdates(symbol string, assembler *BookAssembler) []*schema.Event {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return nil
	}
	p.bookMu.Lock()
	updates := p.pending[symbol]
	delete(p.pending, symbol)
	p.bookMu.Unlock()
	if len(updates) == 0 {
		return nil
	}
	sort.Slice(updates, func(i, j int) bool {
		if updates[i] == nil {
			return false
		}
		if updates[j] == nil {
			return true
		}
		return updates[i].SeqProvider < updates[j].SeqProvider
	})
	result := make([]*schema.Event, 0, len(updates))
	for _, evt := range updates {
		if evt == nil {
			continue
		}
		payload, ok := coerceBookUpdate(evt.Payload)
		if !ok {
			continue
		}
		update, err := assembler.ApplyUpdate(payload, evt.SeqProvider)
		if err != nil {
			if !errors.Is(err, ErrBookNotInitialized) {
				p.emitError(err)
			}
			continue
		}
		evt.Payload = update
		if evt.EmitTS.IsZero() {
			evt.EmitTS = p.clock().UTC()
		}
		result = append(result, evt)
	}
	return result
}

func (p *Provider) emitEvent(evt *schema.Event) {
	if evt == nil {
		return
	}
	if evt.Provider == "" {
		evt.Provider = p.name
	}
	if evt.EmitTS.IsZero() {
		evt.EmitTS = p.clock().UTC()
	}
	extra, emitCurrent, err := p.prepareOrderBook(evt)
	if err != nil {
		p.emitError(err)
		return
	}
	if emitCurrent {
		p.forwardEvent(evt)
	}
	if len(extra) == 0 {
		return
	}
	for _, update := range extra {
		p.forwardEvent(update)
	}
}

func (p *Provider) forwardEvent(evt *schema.Event) {
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

func orderKey(provider, clientOrderID string) string {
	return fmt.Sprintf("%s:%s", strings.ToLower(strings.TrimSpace(provider)), strings.TrimSpace(clientOrderID))
}

func valueOrDefault(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func pollersFromRoute(restFns []dispatcher.RestFn) []RESTPoller {
	if len(restFns) == 0 {
		return nil
	}
	pollers := make([]RESTPoller, 0, len(restFns))
	for _, fn := range restFns {
		pollers = append(pollers, RESTPoller{
			Name:     fn.Name,
			Endpoint: fn.Endpoint,
			Interval: fn.Interval,
			Parser:   fn.Parser,
		})
	}
	return pollers
}

func coerceBookSnapshot(value any) (schema.BookSnapshotPayload, bool) {
	switch v := value.(type) {
	case schema.BookSnapshotPayload:
		return v, true
	case *schema.BookSnapshotPayload:
		if v == nil {
			return schema.BookSnapshotPayload{}, false
		}
		return *v, true
	default:
		return schema.BookSnapshotPayload{}, false
	}
}

func coerceBookUpdate(value any) (schema.BookUpdatePayload, bool) {
	switch v := value.(type) {
	case schema.BookUpdatePayload:
		return v, true
	case *schema.BookUpdatePayload:
		if v == nil {
			return schema.BookUpdatePayload{}, false
		}
		return *v, true
	default:
		return schema.BookUpdatePayload{}, false
	}
}
