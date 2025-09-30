package conductor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

type providerStreams struct {
	events <-chan *schema.Event
	errs   <-chan error
}

// EventOrchestrator coordinates canonical event streams from providers.
type EventOrchestrator struct {
	mu        sync.Mutex
	providers map[string]providerStreams
	events    chan *schema.Event
	errs      chan error
	started   bool
	pools     *pool.PoolManager
}

// NewEventOrchestrator constructs an event orchestrator instance.
func NewEventOrchestrator() *EventOrchestrator {
	return NewEventOrchestratorWithPool(nil)
}

// NewEventOrchestratorWithPool constructs an event orchestrator instance that integrates with a pool manager.
func NewEventOrchestratorWithPool(pools *pool.PoolManager) *EventOrchestrator {
	orchestrator := new(EventOrchestrator)
	orchestrator.providers = make(map[string]providerStreams)
	orchestrator.events = make(chan *schema.Event, 128)
	orchestrator.errs = make(chan error, 8)
	orchestrator.pools = pools
	return orchestrator
}

// AddProvider registers a provider event stream with the orchestrator.
func (o *EventOrchestrator) AddProvider(name string, events <-chan *schema.Event, errs <-chan error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.started {
		return
	}
	if name == "" {
		name = "provider"
	}
	o.providers[name] = providerStreams{events: events, errs: errs}
}

// Events exposes the orchestrated canonical event stream.
func (o *EventOrchestrator) Events() <-chan *schema.Event {
	return o.events
}

// Errors exposes asynchronous orchestration errors.
func (o *EventOrchestrator) Errors() <-chan error {
	return o.errs
}

// Start runs the orchestrator loop until the context is cancelled.
func (o *EventOrchestrator) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("orchestrator requires context")
	}

	o.mu.Lock()
	if o.started {
		o.mu.Unlock()
		return errors.New("orchestrator already started")
	}
	o.started = true
	providers := make([]providerStreams, 0, len(o.providers))
	for _, streams := range o.providers {
		providers = append(providers, streams)
	}
	o.mu.Unlock()

	var wg sync.WaitGroup
	for _, streams := range providers {
		if streams.events != nil {
			wg.Add(1)
			go func(stream <-chan *schema.Event) {
				defer wg.Done()
				o.forwardEvents(ctx, stream)
			}(streams.events)
		}
		if streams.errs != nil {
			wg.Add(1)
			go func(errs <-chan error) {
				defer wg.Done()
				o.forwardErrors(ctx, errs)
			}(streams.errs)
		}
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
	case <-done:
	}

	<-done
	close(o.events)
	close(o.errs)
	return fmt.Errorf("orchestrator context: %w", ctx.Err())
}

func (o *EventOrchestrator) forwardEvents(ctx context.Context, stream <-chan *schema.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-stream:
			if !ok {
				return
			}
			o.handleEvent(ctx, evt)
		}
	}
}

func (o *EventOrchestrator) handleEvent(ctx context.Context, evt *schema.Event) {
	if evt == nil {
		return
	}

	var (
		merged  *schema.MergedEvent
		release func()
	)

	if evt.MergeID != nil {
		var err error
		merged, release, err = o.acquireMergedEvent(ctx)
		if err != nil {
			o.emitError(fmt.Errorf("orchestrator: acquire merged event: %w", err))
			release = nil
		} else {
			o.populateMergedEvent(merged, evt)
		}
	}

	o.dispatchEvent(ctx, evt, release)
}

func (o *EventOrchestrator) acquireMergedEvent(ctx context.Context) (*schema.MergedEvent, func(), error) {
	if o.pools == nil {
		merged := new(schema.MergedEvent)
		return merged, func() {}, nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	getCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	obj, err := o.pools.Get(getCtx, "MergedEvent")
	cancel()
	if err != nil {
		return nil, nil, fmt.Errorf("acquire merged event: %w", err)
	}

	merged, ok := obj.(*schema.MergedEvent)
	if !ok {
		o.pools.Put("MergedEvent", obj)
		return nil, nil, fmt.Errorf("orchestrator: unexpected object type %T", obj)
	}

	release := func() {
		o.pools.Put("MergedEvent", merged)
	}
	return merged, release, nil
}

func (o *EventOrchestrator) populateMergedEvent(merged *schema.MergedEvent, evt *schema.Event) {
	if merged == nil || evt == nil {
		return
	}

	if evt.MergeID != nil {
		merged.MergeID = *evt.MergeID
	}
	merged.Symbol = evt.Symbol
	merged.EventType = evt.Type
	if !evt.IngestTS.IsZero() {
		merged.WindowOpen = evt.IngestTS.UnixNano()
	}
	if !evt.EmitTS.IsZero() {
		merged.WindowClose = evt.EmitTS.UnixNano()
	}
	merged.TraceID = evt.TraceID
	merged.Fragments = append(merged.Fragments, *evt)
	merged.IsComplete = true
}

func (o *EventOrchestrator) dispatchEvent(ctx context.Context, evt *schema.Event, release func()) {
	if release == nil {
		release = func() {}
	}

	select {
	case <-ctx.Done():
		release()
	case o.events <- evt:
		release()
	}
}

func (o *EventOrchestrator) forwardErrors(ctx context.Context, errs <-chan error) {
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-errs:
			if !ok {
				return
			}
			if err == nil {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case o.errs <- err:
			default:
			}
		}
	}
}

func (o *EventOrchestrator) emitError(err error) {
	if err == nil {
		return
	}
	select {
	case o.errs <- err:
	default:
	}
}
