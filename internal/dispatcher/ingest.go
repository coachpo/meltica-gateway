package dispatcher

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coachpo/meltica/errs"
	"github.com/coachpo/meltica/internal/schema"
)

// Ingestor canonicalises raw instances according to the dispatch table.
type Ingestor struct {
	table *Table
	mu    sync.Mutex
	seq   map[string]uint64
}

// NewIngestor constructs a dispatcher ingestor.
func NewIngestor(table *Table) *Ingestor {
	ingestor := new(Ingestor)
	ingestor.table = table
	ingestor.seq = make(map[string]uint64)
	return ingestor
}

// Run consumes raw instances and emits canonical Meltica events.
func (i *Ingestor) Run(ctx context.Context, rawStream <-chan schema.RawInstance) (<-chan schema.MelticaEvent, <-chan error) {
	out := make(chan schema.MelticaEvent)
	errCh := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errCh)
		for {
			select {
			case <-ctx.Done():
				return
			case raw, ok := <-rawStream:
				if !ok {
					return
				}
				if err := i.process(ctx, raw, out); err != nil {
					errCh <- err
				}
			}
		}
	}()

	return out, errCh
}

func (i *Ingestor) process(ctx context.Context, raw schema.RawInstance, out chan<- schema.MelticaEvent) error {
	typeName, ok := toCanonicalType(raw["canonicalType"])
	if !ok {
		return errs.New("dispatcher/ingest", errs.CodeInvalid, errs.WithMessage("missing canonicalType"))
	}
	route, found := i.table.Lookup(typeName)
	if !found {
		return errs.New("dispatcher/ingest", errs.CodeNotFound, errs.WithMessage(fmt.Sprintf("route %s not found", typeName)))
	}
	if !route.Match(raw) {
		return nil
	}
	instrument, _ := raw["instrument"].(string)
	if err := schema.ValidateInstrument(instrument); err != nil {
		return fmt.Errorf("validate instrument: %w", err)
	}
	market, _ := raw["market"].(string)
	if market == "" {
		market = "BINANCE-SPOT"
	}
	source, _ := raw["source"].(string)
	timestamp := extractTimestamp(raw["ts"])
	seq := i.nextSeq(typeName, instrument)
	key := schema.BuildEventKey(instrument, typeName, seq)
	latency := computeLatency(raw["ingestedAt"], timestamp)
	traceID, _ := raw["traceId"].(string)
	payload := raw["payload"]

	evt := schema.MelticaEvent{
		Type:           route.Type,
		Source:         source,
		Ts:             timestamp,
		Instrument:     instrument,
		Market:         market,
		Seq:            seq,
		Key:            key,
		Payload:        payload,
		Latency:        latency,
		TraceID:        traceID,
		RoutingVersion: int(i.table.Version()),
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("ingestor context: %w", ctx.Err())
	case out <- evt:
		return nil
	}
}

func (i *Ingestor) nextSeq(typ schema.CanonicalType, instrument string) uint64 {
	i.mu.Lock()
	defer i.mu.Unlock()
	if instrument == "" {
		instrument = "UNKNOWN"
	}
	key := fmt.Sprintf("%s|%s", typ, instrument)
	seq := i.seq[key] + 1
	i.seq[key] = seq
	return seq
}

func toCanonicalType(value any) (schema.CanonicalType, bool) {
	switch v := value.(type) {
	case schema.CanonicalType:
		return v, true
	case string:
		return schema.CanonicalType(strings.ToUpper(strings.TrimSpace(v))), true
	default:
		return "", false
	}
}

func extractTimestamp(value any) time.Time {
	switch v := value.(type) {
	case time.Time:
		return v
	case int64:
		return time.Unix(0, v*int64(time.Millisecond)).UTC()
	case float64:
		return time.UnixMilli(int64(v)).UTC()
	default:
		return time.Now().UTC()
	}
}

func computeLatency(raw any, event time.Time) time.Duration {
	if event.IsZero() {
		return 0
	}
	if t, ok := raw.(time.Time); ok {
		if t.After(event) {
			return t.Sub(event)
		}
		return time.Since(t)
	}
	return 0
}
