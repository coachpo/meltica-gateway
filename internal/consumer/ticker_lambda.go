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

// TickerLambda specializes in processing ticker events.
type TickerLambda struct {
	id     string
	bus    databus.Bus
	pools  *pool.PoolManager
	logger *log.Logger
}

// NewTickerLambda creates a lambda that processes only ticker events.
func NewTickerLambda(id string, bus databus.Bus, pools *pool.PoolManager, logger *log.Logger) *TickerLambda {
	if id == "" {
		id = "ticker-lambda"
	}
	return &TickerLambda{
		id:     id,
		bus:    bus,
		pools:  pools,
		logger: logger,
	}
}

// Start begins consuming ticker events from the data bus.
func (l *TickerLambda) Start(ctx context.Context) (<-chan error, error) {
	if l.bus == nil {
		return nil, fmt.Errorf("ticker lambda %s: bus required", l.id)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	errs := make(chan error, 1)
	id, ch, err := l.bus.Subscribe(ctx, schema.EventTypeTicker)
	if err != nil {
		close(errs)
		return nil, err
	}

	go l.consume(ctx, databus.SubscriptionID(id), ch, errs)
	return errs, nil
}

func (l *TickerLambda) consume(ctx context.Context, subscriptionID databus.SubscriptionID, events <-chan *schema.Event, errs chan<- error) {
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
			l.handleTickerEvent(evt)
		}
	}
}

func (l *TickerLambda) handleTickerEvent(evt *schema.Event) {
	if evt == nil || evt.Type != schema.EventTypeTicker {
		if l.pools != nil {
			l.pools.RecycleCanonicalEvent(evt)
		}
		return
	}

	payload, ok := evt.Payload.(schema.TickerPayload)
	if !ok {
		if l.pools != nil {
			l.pools.RecycleCanonicalEvent(evt)
		}
		return
	}

	l.printTicker(evt, payload)

	if l.pools != nil {
		l.pools.RecycleCanonicalEvent(evt)
	}
}

func (l *TickerLambda) printTicker(evt *schema.Event, payload schema.TickerPayload) {
	ts := evt.EmitTS
	if ts.IsZero() {
		ts = payload.Timestamp
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	line := fmt.Sprintf(
		"[TICKER:%s] %s Last:%s Bid:%s Ask:%s Volume24h:%s Time:%s",
		l.id,
		evt.Symbol,
		payload.LastPrice,
		payload.BidPrice,
		payload.AskPrice,
		payload.Volume24h,
		ts.Format("15:04:05.000"),
	)

	if l.logger != nil {
		l.logger.Println(line)
	} else {
		fmt.Println(line)
	}
}
