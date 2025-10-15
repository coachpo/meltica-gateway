package consumer

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/bus/databus"
	"github.com/coachpo/meltica/internal/pool"
	"github.com/coachpo/meltica/internal/schema"
)

// OrderBookLambda specializes in processing order book events (snapshots and updates).
type OrderBookLambda struct {
	id     string
	bus    databus.Bus
	pools  *pool.PoolManager
	logger *log.Logger
}

// NewOrderBookLambda creates a lambda that processes only order book events.
func NewOrderBookLambda(id string, bus databus.Bus, pools *pool.PoolManager, logger *log.Logger) *OrderBookLambda {
	if id == "" {
		id = "orderbook-lambda"
	}
	return &OrderBookLambda{
		id:     id,
		bus:    bus,
		pools:  pools,
		logger: logger,
	}
}

// Start begins consuming order book events from the data bus.
func (l *OrderBookLambda) Start(ctx context.Context) (<-chan error, error) {
	if l.bus == nil {
		return nil, fmt.Errorf("orderbook lambda %s: bus required", l.id)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	errs := make(chan error, 2)

	// Subscribe to both book snapshots and updates
	snapshotID, snapshotCh, err := l.bus.Subscribe(ctx, schema.EventTypeBookSnapshot)
	if err != nil {
		close(errs)
		return nil, fmt.Errorf("subscribe to book snapshots: %w", err)
	}

	updateID, updateCh, err := l.bus.Subscribe(ctx, schema.EventTypeBookUpdate)
	if err != nil {
		l.bus.Unsubscribe(snapshotID)
		close(errs)
		return nil, fmt.Errorf("subscribe to book updates: %w", err)
	}

	go l.consumeSnapshots(ctx, databus.SubscriptionID(snapshotID), snapshotCh, errs)
	go l.consumeUpdates(ctx, databus.SubscriptionID(updateID), updateCh, errs)

	return errs, nil
}

func (l *OrderBookLambda) consumeSnapshots(ctx context.Context, subscriptionID databus.SubscriptionID, events <-chan *schema.Event, errs chan<- error) {
	defer func() {
		l.bus.Unsubscribe(subscriptionID)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			l.handleBookSnapshot(evt)
		}
	}
}

func (l *OrderBookLambda) consumeUpdates(ctx context.Context, subscriptionID databus.SubscriptionID, events <-chan *schema.Event, errs chan<- error) {
	defer func() {
		l.bus.Unsubscribe(subscriptionID)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			l.handleBookUpdate(evt)
		}
	}
}

func (l *OrderBookLambda) handleBookSnapshot(evt *schema.Event) {
	if evt == nil || evt.Type != schema.EventTypeBookSnapshot {
		if l.pools != nil {
			l.pools.RecycleCanonicalEvent(evt)
		}
		return
	}

	payload, ok := evt.Payload.(schema.BookSnapshotPayload)
	if !ok {
		if l.pools != nil {
			l.pools.RecycleCanonicalEvent(evt)
		}
		return
	}

	l.printBookSnapshot(evt, payload)

	if l.pools != nil {
		l.pools.RecycleCanonicalEvent(evt)
	}
}

func (l *OrderBookLambda) handleBookUpdate(evt *schema.Event) {
	if evt == nil || evt.Type != schema.EventTypeBookUpdate {
		if l.pools != nil {
			l.pools.RecycleCanonicalEvent(evt)
		}
		return
	}

	payload, ok := evt.Payload.(schema.BookUpdatePayload)
	if !ok {
		if l.pools != nil {
			l.pools.RecycleCanonicalEvent(evt)
		}
		return
	}

	l.printBookUpdate(evt, payload)

	if l.pools != nil {
		l.pools.RecycleCanonicalEvent(evt)
	}
}

func (l *OrderBookLambda) printBookSnapshot(evt *schema.Event, payload schema.BookSnapshotPayload) {
	ts := evt.EmitTS
	if ts.IsZero() {
		ts = payload.LastUpdate
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	line := fmt.Sprintf(
		"[ORDERBOOK:%s] %s SNAPSHOT Bids:%d Asks:%d Checksum:%s Time:%s",
		l.id,
		evt.Symbol,
		len(payload.Bids),
		len(payload.Asks),
		payload.Checksum,
		ts.Format("15:04:05.000"),
	)

	// Print top few levels if available
	topLevels := l.formatTopLevels(payload.Bids, payload.Asks, 3)
	if topLevels != "" {
		line += " " + topLevels
	}

	if l.logger != nil {
		l.logger.Println(line)
	} else {
		fmt.Println(line)
	}
}

func (l *OrderBookLambda) printBookUpdate(evt *schema.Event, payload schema.BookUpdatePayload) {
	ts := evt.EmitTS
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	line := fmt.Sprintf(
		"[ORDERBOOK:%s] %s UPDATE Bids:%d Asks:%d Checksum:%s Time:%s",
		l.id,
		evt.Symbol,
		len(payload.Bids),
		len(payload.Asks),
		payload.Checksum,
		ts.Format("15:04:05.000"),
	)

	// Print changed levels
	changes := l.formatChanges(payload.Bids, payload.Asks)
	if changes != "" {
		line += " " + changes
	}

	if l.logger != nil {
		l.logger.Println(line)
	} else {
		fmt.Println(line)
	}
}

func (l *OrderBookLambda) formatTopLevels(bids, asks []schema.PriceLevel, levels int) string {
	if len(bids) == 0 && len(asks) == 0 {
		return ""
	}

	var parts []string

	// Add top bids
	maxBids := levels
	if maxBids > len(bids) {
		maxBids = len(bids)
	}
	for i := 0; i < maxBids; i++ {
		parts = append(parts, fmt.Sprintf("B:%s@%s", bids[i].Quantity, bids[i].Price))
	}

	// Add top asks
	maxAsks := levels
	if maxAsks > len(asks) {
		maxAsks = len(asks)
	}
	for i := 0; i < maxAsks; i++ {
		parts = append(parts, fmt.Sprintf("A:%s@%s", asks[i].Quantity, asks[i].Price))
	}

	return fmt.Sprintf("[%s]", strings.Join(parts, " "))
}

func (l *OrderBookLambda) formatChanges(bids, asks []schema.PriceLevel) string {
	if len(bids) == 0 && len(asks) == 0 {
		return ""
	}

	var parts []string

	// Add bid changes
	for _, bid := range bids {
		if bid.Quantity == "0.00000000" || bid.Quantity == "0" {
			parts = append(parts, fmt.Sprintf("B:DEL@%s", bid.Price))
		} else {
			parts = append(parts, fmt.Sprintf("B:%s@%s", bid.Quantity, bid.Price))
		}
	}

	// Add ask changes
	for _, ask := range asks {
		if ask.Quantity == "0.00000000" || ask.Quantity == "0" {
			parts = append(parts, fmt.Sprintf("A:DEL@%s", ask.Price))
		} else {
			parts = append(parts, fmt.Sprintf("A:%s@%s", ask.Quantity, ask.Price))
		}
	}

	return fmt.Sprintf("[%s]", strings.Join(parts, " "))
}
