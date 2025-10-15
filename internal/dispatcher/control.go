// Package dispatcher manages routing and control-plane coordination.
package dispatcher

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coachpo/meltica/internal/bus/controlbus"
	"github.com/coachpo/meltica/internal/bus/databus"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

// OrderSubmitter submits outbound orders and exposes query hooks.
type OrderSubmitter interface {
	SubmitOrder(ctx context.Context, req schema.OrderRequest) error
	QueryOrder(ctx context.Context, provider, clientOrderID string) (schema.ExecReport, bool, error)
}

// TradingStateStore records trading enablement flags per consumer.
type TradingStateStore interface {
	Set(consumerID string, enabled bool)
	Enabled(consumerID string) bool
}

// SubscriptionManager defines the contract for managing native adapter subscriptions.
type SubscriptionManager interface {
	Activate(ctx context.Context, route Route) error
	Deactivate(ctx context.Context, typ schema.CanonicalType) error
}

// ControllerOption configures controller dependencies.
type ControllerOption func(*Controller)

// WithOrderSubmitter configures the order submitter dependency.
func WithOrderSubmitter(submitter OrderSubmitter) ControllerOption {
	return func(c *Controller) {
		c.orders = submitter
	}
}

// WithTradingState configures the trading state store dependency.
func WithTradingState(store TradingStateStore) ControllerOption {
	return func(c *Controller) {
		c.trading = store
	}
}

// WithControlPublisher configures the dispatcher to emit control acknowledgements on the data bus.
func WithControlPublisher(bus databus.Bus, pools *pool.PoolManager) ControllerOption {
	return func(c *Controller) {
		c.eventBus = bus
		c.pools = pools
	}
}

// Controller processes control bus commands and mutates the dispatch table.
type Controller struct {
	table   *Table
	bus     controlbus.Bus
	manager SubscriptionManager

	orders  OrderSubmitter
	trading TradingStateStore

	version atomic.Int64

	eventBus databus.Bus
	pools    *pool.PoolManager
}

// NewController creates a dispatcher control controller.
func NewController(table *Table, bus controlbus.Bus, manager SubscriptionManager, opts ...ControllerOption) *Controller {
	controller := new(Controller)
	controller.table = table
	controller.bus = bus
	controller.manager = manager
	for _, opt := range opts {
		if opt != nil {
			opt(controller)
		}
	}
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
			return c.finalizeAck(ctx, msg.Type, ack)
		}
		return c.handleSubscribe(ctx, payload, ack)
	case schema.ControlMessageUnsubscribe:
		var payload schema.Unsubscribe
		if err := msg.DecodePayload(&payload); err != nil {
			ack.ErrorMessage = err.Error()
			return c.finalizeAck(ctx, msg.Type, ack)
		}
		return c.handleUnsubscribe(ctx, payload, ack)

	case schema.ControlMessageSubmitOrder:
		var payload schema.SubmitOrderPayload
		if err := msg.DecodePayload(&payload); err != nil {
			ack.ErrorMessage = err.Error()
			return c.finalizeAck(ctx, msg.Type, ack)
		}
		return c.handleSubmitOrder(ctx, msg.ConsumerID, payload, ack)
	case schema.ControlMessageSetTradingMode:
		var payload schema.TradingModePayload
		if err := msg.DecodePayload(&payload); err != nil {
			ack.ErrorMessage = err.Error()
			return c.finalizeAck(ctx, msg.Type, ack)
		}
		return c.handleTradingMode(ctx, msg.ConsumerID, payload, ack)
	case schema.ControlMessageQueryOrder:
		var payload schema.QueryOrderPayload
		if err := msg.DecodePayload(&payload); err != nil {
			ack.ErrorMessage = err.Error()
			return c.finalizeAck(ctx, msg.Type, ack)
		}
		return c.handleQueryOrder(ctx, payload, ack)
	default:
		ack.ErrorMessage = "unsupported command"
		return c.finalizeAck(ctx, msg.Type, ack)
	}
}

func (c *Controller) handleSubscribe(ctx context.Context, cmd schema.Subscribe, ack schema.ControlAcknowledgement) schema.ControlAcknowledgement {
	typ := cmd.Type
	if err := typ.Validate(); err != nil {
		ack.ErrorMessage = err.Error()
		return c.finalizeAck(ctx, schema.ControlMessageSubscribe, ack)
	}

	route, ok := c.table.Lookup(typ)
	if !ok {
		route = Route{Type: typ}
	}
	if len(cmd.Filters) > 0 {
		route.Filters = mergeFilters(route.Filters, cmd.Filters)
	}
	if err := c.table.Upsert(route); err != nil {
		ack.ErrorMessage = err.Error()
		return c.finalizeAck(ctx, schema.ControlMessageSubscribe, ack)
	}
	if c.manager != nil {
		if err := c.manager.Activate(ctx, route); err != nil {
			ack.ErrorMessage = err.Error()
			return c.finalizeAck(ctx, schema.ControlMessageSubscribe, ack)
		}
	}
	version := c.bumpVersion()
	ack.Success = true
	ack.RoutingVersion = int(version)
	return c.finalizeAck(ctx, schema.ControlMessageSubscribe, ack)
}

func (c *Controller) handleUnsubscribe(ctx context.Context, cmd schema.Unsubscribe, ack schema.ControlAcknowledgement) schema.ControlAcknowledgement {
	typ := cmd.Type
	if err := typ.Validate(); err != nil {
		ack.ErrorMessage = err.Error()
		return c.finalizeAck(ctx, schema.ControlMessageUnsubscribe, ack)
	}
	if _, ok := c.table.Lookup(typ); !ok {
		ack.RoutingVersion = int(c.version.Load())
		ack.ErrorMessage = "no active subscription"
		return c.finalizeAck(ctx, schema.ControlMessageUnsubscribe, ack)
	}
	c.table.Remove(typ)
	if c.manager != nil {
		if err := c.manager.Deactivate(ctx, typ); err != nil {
			ack.ErrorMessage = err.Error()
			return c.finalizeAck(ctx, schema.ControlMessageUnsubscribe, ack)
		}
	}
	version := c.bumpVersion()
	ack.Success = true
	ack.RoutingVersion = int(version)
	return c.finalizeAck(ctx, schema.ControlMessageUnsubscribe, ack)
}

func (c *Controller) handleSubmitOrder(ctx context.Context, consumerID string, payload schema.SubmitOrderPayload, ack schema.ControlAcknowledgement) schema.ControlAcknowledgement {
	if c.orders == nil {
		ack.ErrorMessage = "order submission unavailable"
		return c.finalizeAck(ctx, schema.ControlMessageSubmitOrder, ack)
	}
	consumerID = strings.TrimSpace(consumerID)
	if consumerID == "" {
		ack.ErrorMessage = "consumer id required"
		return ack
	}
	if c.trading != nil && !c.trading.Enabled(consumerID) {
		ack.ErrorMessage = fmt.Sprintf("trading disabled for consumer %s", consumerID)
		return c.finalizeAck(ctx, schema.ControlMessageSubmitOrder, ack)
	}
	if strings.TrimSpace(payload.ClientOrderID) == "" {
		ack.ErrorMessage = "client_order_id required"
		return c.finalizeAck(ctx, schema.ControlMessageSubmitOrder, ack)
	}
	if strings.TrimSpace(payload.Symbol) == "" {
		ack.ErrorMessage = "symbol required"
		return c.finalizeAck(ctx, schema.ControlMessageSubmitOrder, ack)
	}
	if strings.TrimSpace(payload.Quantity) == "" {
		ack.ErrorMessage = "quantity required"
		return c.finalizeAck(ctx, schema.ControlMessageSubmitOrder, ack)
	}
	order := schema.OrderRequest{
		ClientOrderID: payload.ClientOrderID,
		ConsumerID:    consumerID,
		Provider:      strings.TrimSpace(payload.Provider),
		Symbol:        strings.TrimSpace(payload.Symbol),
		Side:          payload.Side,
		OrderType:     payload.OrderType,
		Price:         payload.Price,
		Quantity:      payload.Quantity,
		Timestamp:     payload.Timestamp,
	}
	if order.Provider == "" {
		order.Provider = "binance"
	}
	if order.Timestamp.IsZero() {
		order.Timestamp = time.Now().UTC()
	}
	if err := schema.ValidateInstrument(order.Symbol); err != nil {
		ack.ErrorMessage = err.Error()
		return c.finalizeAck(ctx, schema.ControlMessageSubmitOrder, ack)
	}
	if err := c.orders.SubmitOrder(ctx, order); err != nil {
		ack.ErrorMessage = err.Error()
		return c.finalizeAck(ctx, schema.ControlMessageSubmitOrder, ack)
	}
	version := c.version.Load()
	ack.Success = true
	ack.RoutingVersion = int(version)
	return c.finalizeAck(ctx, schema.ControlMessageSubmitOrder, ack)
}

func (c *Controller) handleTradingMode(ctx context.Context, consumerID string, payload schema.TradingModePayload, ack schema.ControlAcknowledgement) schema.ControlAcknowledgement {
	if c.trading == nil {
		ack.ErrorMessage = "trading state unavailable"
		return c.finalizeAck(ctx, schema.ControlMessageSetTradingMode, ack)
	}
	consumerID = strings.TrimSpace(consumerID)
	if consumerID == "" {
		ack.ErrorMessage = "consumer id required"
		return c.finalizeAck(ctx, schema.ControlMessageSetTradingMode, ack)
	}
	c.trading.Set(consumerID, payload.Enabled)
	version := c.bumpVersion()
	ack.Success = true
	ack.RoutingVersion = int(version)
	return c.finalizeAck(ctx, schema.ControlMessageSetTradingMode, ack)
}

func (c *Controller) handleQueryOrder(ctx context.Context, payload schema.QueryOrderPayload, ack schema.ControlAcknowledgement) schema.ControlAcknowledgement {
	if c.orders == nil {
		ack.ErrorMessage = "order queries unavailable"
		return c.finalizeAck(ctx, schema.ControlMessageQueryOrder, ack)
	}
	clientOrderID := strings.TrimSpace(payload.ClientOrderID)
	if clientOrderID == "" {
		ack.ErrorMessage = "client_order_id required"
		return c.finalizeAck(ctx, schema.ControlMessageQueryOrder, ack)
	}
	provider := strings.TrimSpace(payload.Provider)
	if provider == "" {
		provider = "binance"
	}
	report, found, err := c.orders.QueryOrder(ctx, provider, clientOrderID)
	if err != nil {
		ack.ErrorMessage = err.Error()
		return c.finalizeAck(ctx, schema.ControlMessageQueryOrder, ack)
	}
	if !found {
		ack.ErrorMessage = "order not found"
		return c.finalizeAck(ctx, schema.ControlMessageQueryOrder, ack)
	}
	ack.Success = true
	ack.Result = report
	ack.RoutingVersion = int(c.version.Load())
	return c.finalizeAck(ctx, schema.ControlMessageQueryOrder, ack)
}

func mergeFilters(existing []FilterRule, overrides map[string]any) []FilterRule {
	rules := make([]FilterRule, 0, len(existing)+len(overrides))
	rules = append(rules, existing...)
	for field, value := range overrides {
		rules = append(rules, FilterRule{Field: field, Op: "eq", Value: value})
	}
	return rules
}

func (c *Controller) finalizeAck(ctx context.Context, typ schema.ControlMessageType, ack schema.ControlAcknowledgement) schema.ControlAcknowledgement {
	c.emitControlEvents(ctx, typ, ack)
	return ack
}

func (c *Controller) emitControlEvents(ctx context.Context, typ schema.ControlMessageType, ack schema.ControlAcknowledgement) {
	if c.eventBus == nil {
		return
	}
	payload := schema.ControlAckPayload{
		MessageID:      ack.MessageID,
		ConsumerID:     ack.ConsumerID,
		CommandType:    typ,
		Success:        ack.Success,
		RoutingVersion: ack.RoutingVersion,
		ErrorMessage:   ack.ErrorMessage,
		Timestamp:      ack.Timestamp,
	}
	evt := c.newControlEvent(ctx, schema.EventTypeControlAck, ack.MessageID, payload, ack.RoutingVersion)
	c.publishEvent(ctx, evt)

	if ack.Result != nil {
		resPayload := schema.ControlResultPayload{
			MessageID:      ack.MessageID,
			ConsumerID:     ack.ConsumerID,
			CommandType:    typ,
			RoutingVersion: ack.RoutingVersion,
			Result:         ack.Result,
			Timestamp:      ack.Timestamp,
		}
		resEvt := c.newControlEvent(ctx, schema.EventTypeControlResult, ack.MessageID, resPayload, ack.RoutingVersion)
		c.publishEvent(ctx, resEvt)
	}
}

func (c *Controller) newControlEvent(ctx context.Context, typ schema.EventType, messageID string, payload any, routingVersion int) *schema.Event {
	evt := c.borrowEvent(ctx)
	if evt == nil {
		return nil
	}
	evt.Provider = "dispatcher"
	evt.Type = typ
	evt.Symbol = "CONTROL"
	id := strings.TrimSpace(messageID)
	if id == "" {
		id = fmt.Sprintf("%s-%d", typ, time.Now().UTC().UnixNano())
	}
	evt.EventID = fmt.Sprintf("control:%s:%s", typ, id)
	evt.IngestTS = time.Now().UTC()
	evt.EmitTS = evt.IngestTS
	evt.Payload = payload
	evt.RoutingVersion = routingVersion
	return evt
}

func (c *Controller) publishEvent(ctx context.Context, evt *schema.Event) {
	if evt == nil {
		return
	}
	if c.eventBus != nil {
		if err := c.eventBus.Publish(ctx, evt); err == nil {
			return
		}
	}
	c.recycleEvent(evt)
}

func (c *Controller) borrowEvent(ctx context.Context) *schema.Event {
	evt, err := c.pools.BorrowCanonicalEvent(ctx)
	if err != nil {
		return nil
	}
	return evt
}

func (c *Controller) recycleEvent(evt *schema.Event) {
	if evt == nil {
		return
	}
	if c.pools != nil {
		c.pools.RecycleCanonicalEvent(evt)
	}
}

func (c *Controller) bumpVersion() int64 {
	version := c.version.Add(1)
	if c.table != nil {
		c.table.SetVersion(version)
	}
	return version
}
