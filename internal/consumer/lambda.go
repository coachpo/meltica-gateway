package consumer

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	json "github.com/goccy/go-json"

	"github.com/coachpo/meltica/internal/bus/controlbus"
	"github.com/coachpo/meltica/internal/bus/databus"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
	"github.com/sourcegraph/conc"
)

// Lambda streams events from the data bus and applies a lambda handler before
// recycling them.
type Lambda struct {
	id             string
	bus            databus.Bus
	control        controlbus.Bus
	pools          *pool.PoolManager
	logger         *log.Logger
	routingVersion atomic.Int64
	tradingEnabled atomic.Bool
	consumerID     string
}

// NewLambda constructs a lambda consumer that prints received events.
func NewLambda(id string, bus databus.Bus, pools *pool.PoolManager, logger *log.Logger) *Lambda {
	if id == "" {
		id = "lambda-consumer"
	}
	return &Lambda{
		id:         id,
		bus:        bus,
		pools:      pools,
		logger:     logger,
		consumerID: id,
	}
}

// Start subscribes to the requested event types and prints them to stdout.
func (l *Lambda) Start(ctx context.Context, types []schema.EventType) (<-chan error, error) {
	if l.bus == nil {
		return nil, fmt.Errorf("lambda consumer %s: bus required", l.id)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	typeSet := make(map[schema.EventType]struct{}, len(types)+2)
	subscribeTypes := make([]schema.EventType, 0, len(types)+2)
	for _, typ := range types {
		if _, exists := typeSet[typ]; exists {
			continue
		}
		typeSet[typ] = struct{}{}
		subscribeTypes = append(subscribeTypes, typ)
	}
	for _, typ := range []schema.EventType{schema.EventTypeControlAck, schema.EventTypeControlResult} {
		if _, exists := typeSet[typ]; exists {
			continue
		}
		typeSet[typ] = struct{}{}
		subscribeTypes = append(subscribeTypes, typ)
	}
	errs := make(chan error, len(subscribeTypes))
	subs := make([]subscription, 0, len(subscribeTypes))
	for _, typ := range subscribeTypes {
		id, ch, err := l.bus.Subscribe(ctx, typ)
		if err != nil {
			close(errs)
			for _, sub := range subs {
				l.bus.Unsubscribe(sub.id)
			}
			return nil, err
		}
		subs = append(subs, subscription{id: id, typ: typ, ch: ch})
	}
	go l.consume(ctx, subs, errs)
	return errs, nil
}

// AttachControlBus configures the control bus used for command emission.
func (l *Lambda) AttachControlBus(bus controlbus.Bus, consumerID string) {
	l.control = bus
	if consumerID != "" {
		l.consumerID = consumerID
	}
}

// SendControlMessage sends a control-plane message on behalf of the lambda.
func (l *Lambda) SendControlMessage(ctx context.Context, typ schema.ControlMessageType, payload any) (schema.ControlAcknowledgement, error) {
	if l.control == nil {
		return schema.ControlAcknowledgement{}, fmt.Errorf("lambda %s: control bus unavailable", l.id)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return schema.ControlAcknowledgement{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	msg := schema.ControlMessage{
		MessageID:  fmt.Sprintf("%s-%d", l.id, time.Now().UTC().UnixNano()),
		ConsumerID: l.consumerID,
		Type:       typ,
		Payload:    body,
		Timestamp:  time.Now().UTC(),
	}
	return l.control.Send(ctx, msg)
}

// Subscribe issues a subscribe command via the control bus.
func (l *Lambda) Subscribe(ctx context.Context, payload schema.Subscribe) (schema.ControlAcknowledgement, error) {
	return l.SendControlMessage(ctx, schema.ControlMessageSubscribe, payload)
}

// Unsubscribe issues an unsubscribe command via the control bus.
func (l *Lambda) Unsubscribe(ctx context.Context, payload schema.Unsubscribe) (schema.ControlAcknowledgement, error) {
	return l.SendControlMessage(ctx, schema.ControlMessageUnsubscribe, payload)
}

// SetTradingMode toggles the trading mode for this consumer via control command.
func (l *Lambda) SetTradingMode(ctx context.Context, payload schema.TradingModePayload) (schema.ControlAcknowledgement, error) {
	return l.SendControlMessage(ctx, schema.ControlMessageSetTradingMode, payload)
}

// SubmitOrder submits an order through the control plane.
func (l *Lambda) SubmitOrder(ctx context.Context, payload schema.SubmitOrderPayload) (schema.ControlAcknowledgement, error) {
	return l.SendControlMessage(ctx, schema.ControlMessageSubmitOrder, payload)
}

// QueryOrder queries order status through the control plane.
func (l *Lambda) QueryOrder(ctx context.Context, payload schema.QueryOrderPayload) (schema.ControlAcknowledgement, error) {
	return l.SendControlMessage(ctx, schema.ControlMessageQueryOrder, payload)
}

func (l *Lambda) consume(ctx context.Context, subs []subscription, errs chan<- error) {
	defer close(errs)
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
					l.handleEvent(ctx, sub.typ, evt)
				}
			}
		})
	}
	wg.Wait()
	for _, sub := range subs {
		l.bus.Unsubscribe(sub.id)
	}
}

func (l *Lambda) handleEvent(ctx context.Context, typ schema.EventType, evt *schema.Event) {
	if evt == nil {
		return
	}
	switch typ {
	case schema.EventTypeControlAck:
		l.processControlAck(evt)
		l.recycleIfNeeded(evt)
		return
	case schema.EventTypeControlResult:
		l.processControlResult(evt)
		l.recycleIfNeeded(evt)
		return
	}
	if rv := int64(evt.RoutingVersion); rv > 0 {
		for {
			current := l.routingVersion.Load()
			if rv <= current {
				break
			}
		}
	}
	if l.shouldIgnore(typ, evt) {
		l.recycleIfNeeded(evt)
		return
	}
	l.printEvent(typ, evt)
	l.recycleIfNeeded(evt)
	_ = ctx
}

func (l *Lambda) printEvent(typ schema.EventType, evt *schema.Event) {
	if evt == nil {
		return
	}
	ts := evt.EmitTS
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	payload := fmt.Sprintf("%v", evt.Payload)
	line := fmt.Sprintf(
		"[consumer:%s] type=%s provider=%s symbol=%s seq=%d route=%d trace=%s payload=%s",
		l.id,
		typ,
		evt.Provider,
		evt.Symbol,
		evt.SeqProvider,
		evt.RoutingVersion,
		evt.TraceID,
		payload,
	)
	if l.logger != nil {
		l.logger.Println(line)
		return
	}
	fmt.Println(line)
}

func (l *Lambda) processControlAck(evt *schema.Event) {
	payload, ok := coerceControlAck(evt.Payload)
	if !ok {
		l.logControlAck(schema.ControlAckPayload{
			MessageID:    evt.EventID,
			CommandType:  schema.ControlMessageType(fmt.Sprintf("unknown:%s", evt.Type)),
			ErrorMessage: "invalid control ack payload",
			Timestamp:    time.Now().UTC(),
		})
		return
	}
	if payload.RoutingVersion > 0 {
		l.routingVersion.Store(int64(payload.RoutingVersion))
	}
	if payload.CommandType == schema.ControlMessageSetTradingMode {
		l.tradingEnabled.Store(payload.Success)
	}
	if payload.ConsumerID == "" || payload.ConsumerID == l.consumerID {
		l.logControlAck(payload)
	}
}

func (l *Lambda) processControlResult(evt *schema.Event) {
	payload, ok := coerceControlResult(evt.Payload)
	if !ok {
		l.logControlResult(schema.ControlResultPayload{
			MessageID:   evt.EventID,
			CommandType: schema.ControlMessageType(fmt.Sprintf("unknown:%s", evt.Type)),
			Timestamp:   time.Now().UTC(),
			Result:      "invalid control result payload",
		})
		return
	}
	if payload.ConsumerID == "" || payload.ConsumerID == l.consumerID {
		l.logControlResult(payload)
	}
}

func (l *Lambda) recycleIfNeeded(evt *schema.Event) {
	if evt == nil {
		return
	}
	if l.pools != nil {
		l.pools.RecycleCanonicalEvent(evt)
	}
}

func (l *Lambda) logControlAck(payload schema.ControlAckPayload) {
	line := fmt.Sprintf(
		"[consumer:%s] control-ack id=%s cmd=%s success=%t route=%d error=%s",
		l.id,
		payload.MessageID,
		payload.CommandType,
		payload.Success,
		payload.RoutingVersion,
		payload.ErrorMessage,
	)
	if l.logger != nil {
		l.logger.Println(line)
		return
	}
	fmt.Println(line)
}

func (l *Lambda) logControlResult(payload schema.ControlResultPayload) {
	line := fmt.Sprintf(
		"[consumer:%s] control-result id=%s cmd=%s route=%d result=%v",
		l.id,
		payload.MessageID,
		payload.CommandType,
		payload.RoutingVersion,
		payload.Result,
	)
	if l.logger != nil {
		l.logger.Println(line)
		return
	}
	fmt.Println(line)
}

func (l *Lambda) shouldIgnore(typ schema.EventType, evt *schema.Event) bool {
	if evt == nil {
		return true
	}
	if isCritical(typ) {
		return false
	}
	if !isMarketData(typ) {
		return false
	}
	active := l.routingVersion.Load()
	if active == 0 {
		return false
	}
	return int64(evt.RoutingVersion) < active
}

func isCritical(typ schema.EventType) bool {
	switch typ {
	case schema.EventTypeExecReport,
		schema.EventTypeControlAck,
		schema.EventTypeControlResult:
		return true
	default:
		return false
	}
}

func isMarketData(typ schema.EventType) bool {
	switch typ {
	case schema.EventTypeBookSnapshot,
		schema.EventTypeBookUpdate,
		schema.EventTypeTicker,
		schema.EventTypeKlineSummary,
		schema.EventTypeTrade:
		return true
	default:
		return false
	}
}

// SetRoutingVersion updates the current routing version for this consumer.
func (l *Lambda) SetRoutingVersion(version int64) {
	l.routingVersion.Store(version)
}

// GetRoutingVersion returns the current routing version for this consumer.
func (l *Lambda) GetRoutingVersion() int64 {
	return l.routingVersion.Load()
}

// TradingEnabled returns the last known trading state acknowledged by the control plane.
func (l *Lambda) TradingEnabled() bool {
	return l.tradingEnabled.Load()
}

func coerceControlAck(value any) (schema.ControlAckPayload, bool) {
	switch v := value.(type) {
	case schema.ControlAckPayload:
		return v, true
	case *schema.ControlAckPayload:
		if v == nil {
			return schema.ControlAckPayload{}, false
		}
		return *v, true
	default:
		return schema.ControlAckPayload{}, false
	}
}

func coerceControlResult(value any) (schema.ControlResultPayload, bool) {
	switch v := value.(type) {
	case schema.ControlResultPayload:
		return v, true
	case *schema.ControlResultPayload:
		if v == nil {
			return schema.ControlResultPayload{}, false
		}
		return *v, true
	default:
		return schema.ControlResultPayload{}, false
	}
}
