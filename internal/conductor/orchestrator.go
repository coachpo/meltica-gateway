package conductor

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/coachpo/meltica/errs"
	"github.com/coachpo/meltica/internal/schema"
	"github.com/coachpo/meltica/internal/snapshot"
)

// Orchestrator fuses websocket deltas with snapshot state before publication.
type Orchestrator struct {
	store    snapshot.Store
	throttle *Throttle
	mu       sync.Mutex
	seq      map[string]uint64
}

// NewOrchestrator creates a new orchestrator instance.
func NewOrchestrator(store snapshot.Store, throttle *Throttle) *Orchestrator {
	if throttle == nil {
		throttle = NewThrottle(0)
	}
	orchestrator := new(Orchestrator)
	orchestrator.store = store
	orchestrator.throttle = throttle
	orchestrator.seq = make(map[string]uint64)
	return orchestrator
}

// Run consumes canonical events and emits fused snapshots downstream.
func (o *Orchestrator) Run(ctx context.Context, in <-chan schema.MelticaEvent) (<-chan schema.MelticaEvent, <-chan error) {
	out := make(chan schema.MelticaEvent)
	errCh := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errCh)
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-in:
				if !ok {
					return
				}
				fused, err := o.process(ctx, evt)
				if err != nil {
					errCh <- err
					continue
				}
				if fused == nil {
					continue
				}
				for _, fe := range fused {
					if !o.throttle.Allow(fe.Instrument) {
						continue
					}
					select {
					case <-ctx.Done():
						return
					case out <- fe:
					}
				}
			}
		}
	}()
	return out, errCh
}

func (o *Orchestrator) process(ctx context.Context, evt schema.MelticaEvent) ([]schema.MelticaEvent, error) {
	switch evt.Type {
	case schema.CanonicalType("ORDERBOOK.SNAPSHOT"):
		return o.handleSnapshot(ctx, evt)
	case schema.CanonicalType("ORDERBOOK.DELTA"):
		return o.handleDelta(ctx, evt)
	default:
		return []schema.MelticaEvent{evt}, nil
	}
}

func (o *Orchestrator) handleSnapshot(ctx context.Context, evt schema.MelticaEvent) ([]schema.MelticaEvent, error) {
	var record snapshot.Record
	record.Key = snapshot.Key{Market: evt.Market, Instrument: evt.Instrument, Type: evt.Type}
	record.Seq = evt.Seq
	record.Version = evt.Seq
	record.Data = copyPayload(evt.Payload)
	record.UpdatedAt = evt.Ts
	if _, err := o.store.Put(ctx, record); err != nil {
		return nil, fmt.Errorf("orchestrator store put: %w", err)
	}
	return []schema.MelticaEvent{o.decorate(evt)}, nil
}

func (o *Orchestrator) handleDelta(ctx context.Context, evt schema.MelticaEvent) ([]schema.MelticaEvent, error) {
	key := snapshot.Key{Market: evt.Market, Instrument: evt.Instrument, Type: schema.CanonicalType("ORDERBOOK.SNAPSHOT")}
	current, err := o.store.Get(ctx, key)
	if err != nil {
		if isErrCode(err, errs.CodeNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("orchestrator store get: %w", err)
	}
	updated := current
	updated.Seq = current.Seq + 1
	updated.UpdatedAt = evt.Ts
	applyDelta(updated.Data, evt.Payload)

	for retries := 0; retries < 3; retries++ {
		res, err := o.store.CompareAndSwap(ctx, current.Version, updated)
		if err == nil {
			fused := o.decorate(snapshotEventFromRecord(evt, res))
			return []schema.MelticaEvent{fused}, nil
		}
		if !isErrCode(err, errs.CodeConflict) {
			return nil, fmt.Errorf("orchestrator store cas: %w", err)
		}
		current, err = o.store.Get(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("orchestrator store get: %w", err)
		}
		updated = current
		updated.Seq = current.Seq + 1
		updated.UpdatedAt = evt.Ts
		applyDelta(updated.Data, evt.Payload)
	}
	return nil, errs.New("conductor/orchestrator", errs.CodeConflict, errs.WithMessage("cas retries exceeded"))
}

func (o *Orchestrator) decorate(evt schema.MelticaEvent) schema.MelticaEvent {
	seq := o.nextSeq(evt.Instrument, evt.Type)
	evt.Seq = seq
	evt.Key = schema.BuildEventKey(evt.Instrument, evt.Type, seq)
	if evt.Latency == 0 && !evt.Ts.IsZero() {
		evt.Latency = time.Since(evt.Ts)
	}
	return evt
}

func (o *Orchestrator) nextSeq(instrument string, typ schema.CanonicalType) uint64 {
	key := fmt.Sprintf("%s|%s", instrument, typ)
	o.mu.Lock()
	defer o.mu.Unlock()
	seq := o.seq[key] + 1
	o.seq[key] = seq
	return seq
}

func copyPayload(payload any) map[string]any {
	m, ok := payload.(map[string]any)
	if !ok {
		return map[string]any{"value": payload}
	}
	clone := make(map[string]any, len(m))
	for k, v := range m {
		clone[k] = v
	}
	return clone
}

func applyDelta(snapshot map[string]any, delta any) {
	m, ok := delta.(map[string]any)
	if !ok {
		return
	}
	side, _ := m["side"].(string)
	price, _ := toFloat(m["price"])
	qty, _ := toFloat(m["qty"])
	switch side {
	case "bid":
		snapshot["topBid"] = price
		snapshot["bidQty"] = qty
	case "ask":
		snapshot["topAsk"] = price
		snapshot["askQty"] = qty
	}
}

func toFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f, true
		}
	}
	return 0, false
}

func snapshotEventFromRecord(base schema.MelticaEvent, record snapshot.Record) schema.MelticaEvent {
	var evt schema.MelticaEvent
	evt.Type = schema.CanonicalType("ORDERBOOK.SNAPSHOT")
	evt.Source = base.Source
	evt.Instrument = record.Key.Instrument
	evt.Market = record.Key.Market
	evt.Ts = record.UpdatedAt
	evt.Payload = copyPayload(record.Data)
	evt.TraceID = base.TraceID
	return evt
}

func isErrCode(err error, code errs.Code) bool {
	if e, ok := err.(*errs.E); ok {
		return e.Code == code
	}
	return false
}
