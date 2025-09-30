package binance

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coachpo/meltica/internal/dispatcher"
	"github.com/coachpo/meltica/internal/schema"
)

// ProviderOptions configure the Binance provider runtime.
type ProviderOptions struct {
	Topics    []string
	Snapshots []RESTPoller
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

	started atomic.Bool

	mu     sync.Mutex
	routes map[string]*routeHandle
	ctx    context.Context
	cancel context.CancelFunc
}

type routeHandle struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
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
	provider.events = make(chan *schema.Event, 128)
	provider.errs = make(chan error, 8)
	provider.routes = make(map[string]*routeHandle)
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
	}()

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
		handle.wg.Add(1)
		go func() {
			defer handle.wg.Done()
			p.pipeEvents(routeCtx, events)
		}()
		if errs != nil {
			handle.wg.Add(1)
			go func() {
				defer handle.wg.Done()
				p.pipeErrors(routeCtx, errs)
			}()
		}
	}

	if len(pollers) > 0 && p.rest != nil {
		events, errs := p.rest.Poll(routeCtx, pollers)
		handle.wg.Add(1)
		go func() {
			defer handle.wg.Done()
			p.pipeEvents(routeCtx, events)
		}()
		if errs != nil {
			handle.wg.Add(1)
			go func() {
				defer handle.wg.Done()
				p.pipeErrors(routeCtx, errs)
			}()
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
