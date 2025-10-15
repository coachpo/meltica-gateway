// Package consumer exposes data bus consumer utilities.
package consumer

import (
	"context"
	"log"
	"time"

	"github.com/coachpo/meltica/internal/bus/databus"
	"github.com/coachpo/meltica/internal/schema"
	"github.com/sourcegraph/conc"
)

// Consumer subscribes to dispatcher events over the data bus and exposes them downstream.
type Consumer struct {
	ID     string
	bus    databus.Bus
	logger *log.Logger

	events chan *schema.Event
	err    chan error
}

type subscription struct {
	id  databus.SubscriptionID
	typ schema.EventType
	ch  <-chan *schema.Event
}

// NewConsumer constructs a consumer bound to the shared data bus.
func NewConsumer(id string, bus databus.Bus, logger *log.Logger) *Consumer {
	consumer := new(Consumer)
	consumer.ID = id
	consumer.bus = bus
	consumer.logger = logger
	return consumer
}

// Start subscribes to the requested event types and begins forwarding events until the context is cancelled.
func (c *Consumer) Start(ctx context.Context, types []schema.EventType) (<-chan *schema.Event, <-chan error) {
	if c.events != nil {
		return c.events, c.err
	}
	c.events = make(chan *schema.Event, 256)
	c.err = make(chan error, 1)
	go c.consume(ctx, types)
	return c.events, c.err
}

// consume forwards heap-cloned dispatcher events downstream; clones must never
// be returned to any pool.
func (c *Consumer) consume(ctx context.Context, types []schema.EventType) {
	defer close(c.events)
	defer close(c.err)

	subs := make([]subscription, 0, len(types))
	for _, typ := range types {
		id, ch, err := c.bus.Subscribe(ctx, typ)
		if err != nil {
			c.err <- err
			for _, existing := range subs {
				c.bus.Unsubscribe(existing.id)
			}
			return
		}
		subs = append(subs, subscription{id: id, typ: typ, ch: ch})
	}

	var wg conc.WaitGroup
	for _, sub := range subs {
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case evt, ok := <-sub.ch:
					if !ok {
						return
					}
					if evt == nil {
						continue
					}
					c.logEvent(sub.typ, evt)
					select {
					case <-ctx.Done():
						return
					case c.events <- evt:
					}
				}
			}
		})
	}

	<-ctx.Done()

	for _, sub := range subs {
		c.bus.Unsubscribe(sub.id)
	}

	wg.Wait()
}

func (c *Consumer) logEvent(typ schema.EventType, evt *schema.Event) {
	if evt == nil {
		return
	}
	if c.logger == nil {
		return
	}
	traceID := evt.TraceID
	if traceID == "" {
		if payload, ok := evt.Payload.(map[string]any); ok {
			if v, ok := payload["trace_id"].(string); ok && v != "" {
				traceID = v
			} else if v, ok := payload["traceId"].(string); ok && v != "" {
				traceID = v
			}
		}
	}
	c.logger.Printf("consumer %s received %s event=%s trace=%s at %s", c.ID, typ, evt.EventID, traceID, time.Now().UTC().Format(time.RFC3339Nano))
}
