// Package dispatcher manages routing and control-plane coordination.
package dispatcher

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/coachpo/meltica/internal/bus/controlbus"
	"github.com/coachpo/meltica/internal/schema"
)

// SubscriptionManager defines the contract for managing native adapter subscriptions.
type SubscriptionManager interface {
	Activate(ctx context.Context, route Route) error
	Deactivate(ctx context.Context, typ schema.CanonicalType) error
}

// Controller processes control bus commands and mutates the dispatch table.
type Controller struct {
	table   *Table
	bus     controlbus.Bus
	manager SubscriptionManager
	version atomic.Int64
}

// NewController creates a dispatcher control controller.
func NewController(table *Table, bus controlbus.Bus, manager SubscriptionManager) *Controller {
	controller := new(Controller)
	controller.table = table
	controller.bus = bus
	controller.manager = manager
	return controller
}

// Start begins consuming control bus commands until the context is cancelled.
func (c *Controller) Start(ctx context.Context) error {
	messages, err := c.bus.Consume(ctx)
	if err != nil {
		return fmt.Errorf("consume control bus: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("controller context: %w", ctx.Err())
		case msg, ok := <-messages:
			if !ok {
				return nil
			}
			ack := c.handle(ctx, msg.Command)
			if msg.Reply != nil {
				msg.Reply <- ack
			}
		}
	}
}

func (c *Controller) handle(ctx context.Context, msg schema.ControlMessage) schema.ControlAcknowledgement {
	var ack schema.ControlAcknowledgement
	ack.MessageID = msg.MessageID
	ack.ConsumerID = msg.ConsumerID
	ack.Timestamp = time.Now().UTC()
	switch msg.Type {
	case schema.ControlMessageSubscribe:
		var payload schema.Subscribe
		if err := msg.DecodePayload(&payload); err != nil {
			ack.ErrorMessage = err.Error()
			return ack
		}
		return c.handleSubscribe(ctx, payload, ack)
	case schema.ControlMessageUnsubscribe:
		var payload schema.Unsubscribe
		if err := msg.DecodePayload(&payload); err != nil {
			ack.ErrorMessage = err.Error()
			return ack
		}
		return c.handleUnsubscribe(ctx, payload, ack)
	case schema.ControlMessageMergedSubscribe:
		ack.ErrorMessage = "merged subscriptions not supported"
		return ack
	case schema.ControlMessageSetTradingMode:
		ack.ErrorMessage = "trading mode updates not supported"
		return ack
	default:
		ack.ErrorMessage = "unsupported command"
		return ack
	}
}

func (c *Controller) handleSubscribe(ctx context.Context, cmd schema.Subscribe, ack schema.ControlAcknowledgement) schema.ControlAcknowledgement {
	typ := cmd.Type
	if err := typ.Validate(); err != nil {
		ack.ErrorMessage = err.Error()
		return ack
	}
	route, ok := c.table.Lookup(typ)
	if !ok {
		var newRoute Route
		newRoute.Type = typ
		route = newRoute
	}
	if len(cmd.Filters) > 0 {
		route.Filters = mergeFilters(route.Filters, cmd.Filters)
	}
	if err := c.table.Upsert(route); err != nil {
		ack.ErrorMessage = err.Error()
		return ack
	}
	if c.manager != nil {
		if err := c.manager.Activate(ctx, route); err != nil {
			ack.ErrorMessage = err.Error()
			return ack
		}
	}
	version := c.version.Add(1)
	c.table.SetVersion(version)
	ack.Success = true
	ack.RoutingVersion = int(version)
	return ack
}

func (c *Controller) handleUnsubscribe(ctx context.Context, cmd schema.Unsubscribe, ack schema.ControlAcknowledgement) schema.ControlAcknowledgement {
	typ := cmd.Type
	if err := typ.Validate(); err != nil {
		ack.ErrorMessage = err.Error()
		return ack
	}
	if _, ok := c.table.Lookup(typ); !ok {
		ack.Success = false
		ack.RoutingVersion = int(c.version.Load())
		ack.ErrorMessage = "no active subscription"
		return ack
	}
	c.table.Remove(typ)
	if c.manager != nil {
		if err := c.manager.Deactivate(ctx, typ); err != nil {
			ack.ErrorMessage = err.Error()
			return ack
		}
	}
	version := c.version.Add(1)
	c.table.SetVersion(version)
	ack.Success = true
	ack.RoutingVersion = int(version)
	return ack
}

func mergeFilters(existing []FilterRule, overrides map[string]any) []FilterRule {
	rules := make([]FilterRule, 0, len(existing)+len(overrides))
	rules = append(rules, existing...)
	for field, value := range overrides {
		rules = append(rules, FilterRule{Field: field, Op: "eq", Value: value})
	}
	return rules
}
