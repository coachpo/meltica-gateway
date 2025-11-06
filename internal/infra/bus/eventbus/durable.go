package eventbus

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coachpo/meltica/internal/domain/outboxstore"
	"github.com/coachpo/meltica/internal/domain/schema"
	"github.com/coachpo/meltica/internal/infra/pool"
	json "github.com/goccy/go-json"
)

// DurableOption configures the durable bus wrapper.
type DurableOption func(*DurableBus)

// WithDurableLogger overrides the default logger used by the durable bus.
func WithDurableLogger(logger *log.Logger) DurableOption {
	return func(b *DurableBus) {
		if logger != nil {
			b.logger = logger
		}
	}
}

// WithReplayInterval tweaks the polling cadence for replaying undelivered events.
func WithReplayInterval(interval time.Duration) DurableOption {
	return func(b *DurableBus) {
		if interval > 0 {
			b.replayInterval = interval
		}
	}
}

// WithReplayBatchSize configures the number of rows fetched per replay tick.
func WithReplayBatchSize(size int) DurableOption {
	return func(b *DurableBus) {
		if size > 0 {
			b.replayBatchSize = size
		}
	}
}

// WithReplayDisabled skips starting the background replay worker.
func WithReplayDisabled() DurableOption {
	return func(b *DurableBus) {
		b.replayDisabled = true
	}
}

// WithDurablePoolManager injects the pool manager used when reconstructing replay events.
func WithDurablePoolManager(pools *pool.PoolManager) DurableOption {
	return func(b *DurableBus) {
		if pools != nil {
			b.pools = pools
		}
	}
}

type poolAwareBus interface {
	PoolManager() *pool.PoolManager
}

// DurableBus wraps an event bus with outbox-backed durability guarantees.
type DurableBus struct {
	inner Bus
	store outboxstore.Store

	logger          *log.Logger
	replayInterval  time.Duration
	replayBatchSize int
	replayDisabled  bool

	pools *pool.PoolManager

	replayCtx    context.Context
	replayCancel context.CancelFunc
	replayWG     sync.WaitGroup
}

const (
	defaultReplayInterval  = 5 * time.Second
	defaultReplayBatchSize = 128
)

// NewDurableBus wraps the provided bus with outbox persistence. When store is nil the
// original bus is returned unmodified.
func NewDurableBus(inner Bus, store outboxstore.Store, opts ...DurableOption) Bus {
	if inner == nil {
		return nil
	}
	if store == nil {
		return inner
	}
	durable := &DurableBus{
		inner:           inner,
		store:           store,
		logger:          log.New(os.Stdout, "eventbus/durable ", log.LstdFlags|log.Lmicroseconds),
		replayInterval:  defaultReplayInterval,
		replayBatchSize: defaultReplayBatchSize,
		replayDisabled:  false,
		pools:           nil,
		replayCtx:       nil,
		replayCancel:    nil,
		replayWG:        sync.WaitGroup{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(durable)
		}
	}
	if durable.pools == nil {
		if aware, ok := inner.(poolAwareBus); ok {
			durable.pools = aware.PoolManager()
		}
	}
	if durable.replayBatchSize <= 0 {
		durable.replayBatchSize = defaultReplayBatchSize
	}
	if durable.replayInterval <= 0 {
		durable.replayInterval = defaultReplayInterval
	}
	if !durable.replayDisabled {
		durable.startReplayWorker()
	}
	return durable
}

// Publish persists the event to the outbox before delegating to the inner bus.
func (b *DurableBus) Publish(ctx context.Context, evt *schema.Event) error {
	if b == nil || b.store == nil {
		if b == nil {
			return nil
		}
		if b.inner == nil {
			return fmt.Errorf("durable bus: inner bus unavailable")
		}
		if err := b.inner.Publish(ctx, evt); err != nil {
			return fmt.Errorf("durable bus passthrough publish: %w", err)
		}
		return nil
	}
	recordID, err := b.enqueueEvent(ctx, evt)
	if err != nil {
		return err
	}
	if err := b.inner.Publish(ctx, evt); err != nil {
		b.markFailure(ctx, recordID, err)
		return fmt.Errorf("durable bus publish: %w", err)
	}
	if err := b.store.MarkDelivered(safeContext(ctx), recordID); err != nil {
		b.logf("mark delivered failed: %v", err)
		return fmt.Errorf("durable bus mark delivered: %w", err)
	}
	return nil
}

// Subscribe delegates to the inner bus.
func (b *DurableBus) Subscribe(ctx context.Context, typ schema.EventType) (SubscriptionID, <-chan *schema.Event, error) {
	if b == nil || b.inner == nil {
		return "", nil, fmt.Errorf("durable bus: inner bus unavailable")
	}
	id, ch, err := b.inner.Subscribe(ctx, typ)
	if err != nil {
		return "", nil, fmt.Errorf("durable bus subscribe: %w", err)
	}
	return id, ch, nil
}

// Unsubscribe delegates to the inner bus.
func (b *DurableBus) Unsubscribe(id SubscriptionID) {
	if b == nil || b.inner == nil {
		return
	}
	b.inner.Unsubscribe(id)
}

// Close stops the replay worker before closing the inner bus.
func (b *DurableBus) Close() {
	if b == nil {
		return
	}
	if b.replayCancel != nil {
		b.replayCancel()
		b.replayWG.Wait()
	}
	if b.inner != nil {
		b.inner.Close()
	}
}

func (b *DurableBus) startReplayWorker() {
	if b.store == nil || b.inner == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.replayCtx = ctx
	b.replayCancel = cancel
	b.replayWG.Add(1)
	go func() {
		defer b.replayWG.Done()
		ticker := time.NewTicker(b.replayInterval)
		defer ticker.Stop()
		b.replayPendingEvents()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				b.replayPendingEvents()
			}
		}
	}()
}

func (b *DurableBus) replayPendingEvents() {
	ctx := b.replayCtx
	if ctx == nil {
		ctx = context.Background()
	}
	records, err := b.store.ListPending(ctx, b.replayBatchSize)
	if err != nil {
		b.logf("outbox replay list failed: %v", err)
		return
	}
	for _, record := range records {
		event, err := b.prepareReplayEvent(ctx, record.Payload)
		if err != nil {
			b.logf("outbox replay decode failed (id=%d): %v", record.ID, err)
			_ = b.store.MarkFailed(ctx, record.ID, err.Error())
			continue
		}
		if err := b.inner.Publish(ctx, event); err != nil {
			b.logf("outbox replay publish failed (id=%d): %v", record.ID, err)
			_ = b.store.MarkFailed(ctx, record.ID, err.Error())
			continue
		}
		if err := b.store.MarkDelivered(ctx, record.ID); err != nil {
			b.logf("outbox replay mark delivered failed (id=%d): %v", record.ID, err)
		}
	}
}

func (b *DurableBus) prepareReplayEvent(ctx context.Context, payload json.RawMessage) (*schema.Event, error) {
	decoded, err := rawToEvent(payload)
	if err != nil {
		return nil, err
	}
	if b.pools == nil {
		return decoded, nil
	}
	evt, borrowErr := b.pools.BorrowEventInst(ctx)
	if borrowErr != nil {
		return nil, fmt.Errorf("outbox replay borrow event: %w", borrowErr)
	}
	schema.CopyEvent(evt, decoded)
	return evt, nil
}

func (b *DurableBus) enqueueEvent(ctx context.Context, evt *schema.Event) (int64, error) {
	if evt == nil {
		return 0, fmt.Errorf("durable bus: event required")
	}
	payload, err := eventToJSON(evt)
	if err != nil {
		return 0, fmt.Errorf("durable bus: encode payload: %w", err)
	}
	headers := map[string]any{}
	if trimmed := strings.TrimSpace(evt.Provider); trimmed != "" {
		headers["provider"] = trimmed
	}
	if trimmed := strings.TrimSpace(evt.Symbol); trimmed != "" {
		headers["symbol"] = trimmed
	}
	if trimmed := strings.TrimSpace(evt.EventID); trimmed != "" {
		headers["eventId"] = trimmed
	}
	aggregateType, aggregateID := aggregateIdentifiers(evt)
	record, err := b.store.Enqueue(safeContext(ctx), outboxstore.Event{
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		EventType:     string(evt.Type),
		Payload:       payload,
		Headers:       headers,
		AvailableAt:   evt.EmitTS,
	})
	if err != nil {
		return 0, fmt.Errorf("durable bus enqueue: %w", err)
	}
	return record.ID, nil
}

func (b *DurableBus) markFailure(ctx context.Context, id int64, publishErr error) {
	if b.store == nil || id == 0 {
		return
	}
	msg := "publish failed"
	if publishErr != nil && strings.TrimSpace(publishErr.Error()) != "" {
		msg = publishErr.Error()
	}
	if err := b.store.MarkFailed(safeContext(ctx), id, msg); err != nil {
		b.logf("outbox mark failed error: %v", err)
	}
}

func (b *DurableBus) logf(format string, args ...any) {
	if b.logger == nil {
		return
	}
	b.logger.Printf(format, args...)
}

func eventToJSON(evt *schema.Event) (json.RawMessage, error) {
	if evt == nil {
		return nil, fmt.Errorf("nil event")
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	return json.RawMessage(data), nil
}

func rawToEvent(payload json.RawMessage) (*schema.Event, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty payload")
	}
	var evt schema.Event
	if err := json.Unmarshal(payload, &evt); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	rehydrateEventPayload(&evt)
	return &evt, nil
}

func aggregateIdentifiers(evt *schema.Event) (string, string) {
	aggType := strings.ToLower(strings.TrimSpace(evt.Provider))
	if aggType == "" {
		aggType = strings.ToLower(strings.TrimSpace(string(evt.Type)))
	}
	if aggType == "" {
		aggType = "eventbus"
	}
	aggID := strings.TrimSpace(evt.EventID)
	if aggID == "" {
		if route, ok := schema.PrimaryRouteForEvent(evt.Type); ok {
			aggID = schema.BuildEventKey(evt.Symbol, route, evt.SeqProvider)
		}
	}
	if aggID == "" {
		aggID = fmt.Sprintf("%s:%s:%d", strings.TrimSpace(evt.Provider), strings.TrimSpace(evt.Symbol), evt.SeqProvider)
	}
	return aggType, aggID
}

func safeContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func rehydrateEventPayload(evt *schema.Event) {
	if evt == nil {
		return
	}
	rawMap, ok := evt.Payload.(map[string]any)
	if !ok {
		return
	}
	data, err := json.Marshal(rawMap)
	if err != nil {
		return
	}
	switch evt.Type {
	case schema.EventTypeTrade:
		var payload schema.TradePayload
		if json.Unmarshal(data, &payload) == nil {
			evt.Payload = payload
		}
	case schema.EventTypeTicker:
		var payload schema.TickerPayload
		if json.Unmarshal(data, &payload) == nil {
			evt.Payload = payload
		}
	case schema.EventTypeBookSnapshot:
		var payload schema.BookSnapshotPayload
		if json.Unmarshal(data, &payload) == nil {
			evt.Payload = payload
		}
	case schema.EventTypeExecReport:
		var payload schema.ExecReportPayload
		if json.Unmarshal(data, &payload) == nil {
			evt.Payload = payload
		}
	case schema.EventTypeKlineSummary:
		var payload schema.KlineSummaryPayload
		if json.Unmarshal(data, &payload) == nil {
			evt.Payload = payload
		}
	case schema.EventTypeInstrumentUpdate:
		var payload schema.InstrumentUpdatePayload
		if json.Unmarshal(data, &payload) == nil {
			evt.Payload = payload
		}
	case schema.EventTypeBalanceUpdate:
		var payload schema.BalanceUpdatePayload
		if json.Unmarshal(data, &payload) == nil {
			evt.Payload = payload
		}
	case schema.EventTypeRiskControl:
		var payload schema.RiskControlPayload
		if json.Unmarshal(data, &payload) == nil {
			evt.Payload = payload
		}
	}
}

var _ Bus = (*DurableBus)(nil)
