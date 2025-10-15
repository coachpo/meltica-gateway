package consumer

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/coachpo/meltica/internal/bus/databus"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

// TradeLambda specializes in processing trade events.
type TradeLambda struct {
	id     string
	bus    databus.Bus
	pools  *pool.PoolManager
	logger *log.Logger
}

// NewTradeLambda creates a lambda that processes only trade events.
func NewTradeLambda(id string, bus databus.Bus, pools *pool.PoolManager, logger *log.Logger) *TradeLambda {
	if id == "" {
		id = "trade-lambda"
	}
	return &TradeLambda{
		id:     id,
		bus:    bus,
		pools:  pools,
		logger: logger,
	}
}

// Start begins consuming trade events from the data bus.
func (l *TradeLambda) Start(ctx context.Context) (<-chan error, error) {
	if l.bus == nil {
		return nil, fmt.Errorf("trade lambda %s: bus required", l.id)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	errs := make(chan error, 1)
	id, ch, err := l.bus.Subscribe(ctx, schema.EventTypeTrade)
	if err != nil {
		close(errs)
		return nil, err
	}

	go l.consume(ctx, databus.SubscriptionID(id), ch, errs)
	return errs, nil
}

func (l *TradeLambda) consume(ctx context.Context, subscriptionID databus.SubscriptionID, events <-chan *schema.Event, errs chan<- error) {
	defer close(errs)
	defer l.bus.Unsubscribe(subscriptionID)

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			l.handleTradeEvent(evt)
		}
	}
}

func (l *TradeLambda) handleTradeEvent(evt *schema.Event) {
	if evt == nil || evt.Type != schema.EventTypeTrade {
		if l.pools != nil {
			l.pools.RecycleCanonicalEvent(evt)
		}
		return
	}

	payload, ok := evt.Payload.(schema.TradePayload)
	if !ok {
		if l.pools != nil {
			l.pools.RecycleCanonicalEvent(evt)
		}
		return
	}

	l.printTrade(evt, payload)

	if l.pools != nil {
		l.pools.RecycleCanonicalEvent(evt)
	}
}

func (l *TradeLambda) printTrade(evt *schema.Event, payload schema.TradePayload) {
	ts := evt.EmitTS
	if ts.IsZero() {
		ts = payload.Timestamp
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	sideStr := "BUY"
	if payload.Side == schema.TradeSideSell {
		sideStr = "SELL"
	}

	line := fmt.Sprintf(
		"[TRADE:%s] %s %s %s@%s ID:%s Time:%s",
		l.id,
		evt.Symbol,
		sideStr,
		payload.Quantity,
		payload.Price,
		payload.TradeID,
		ts.Format("15:04:05.000"),
	)

	if l.logger != nil {
		l.logger.Println(line)
	} else {
		fmt.Println(line)
	}
}
