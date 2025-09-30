// Package conductor provides orchestration utilities for canonical events.
package conductor

import (
	"context"
	"time"

	"github.com/coachpo/meltica/internal/bus/databus"
	"github.com/coachpo/meltica/internal/schema"
)

// Forwarder streams canonical events onto the data bus.
type Forwarder struct {
	bus databus.Bus
}

// NewForwarder creates a conductor forwarder.
func NewForwarder(bus databus.Bus) *Forwarder {
	return &Forwarder{bus: bus}
}

// Run forwards events until context cancellation.
func (f *Forwarder) Run(ctx context.Context, events <-chan schema.MelticaEvent) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-events:
				if !ok {
					return
				}
				converted := convertEvent(evt)
				if err := f.bus.Publish(ctx, converted); err != nil {
					errCh <- err
				}
			}
		}
	}()
	return errCh
}

func convertEvent(evt schema.MelticaEvent) *schema.Event {
	converted := schema.Event{
		EventID:        evt.Key,
		MergeID:        nil,
		RoutingVersion: evt.RoutingVersion,
		Provider:       evt.Source,
		Symbol:         evt.Instrument,
		Type:           mapCanonicalEventType(evt.Type),
		SeqProvider:    evt.Seq,
		IngestTS:       evt.Ts,
		EmitTS:         time.Now().UTC(),
		Payload:        evt.Payload,
		TraceID:        evt.TraceID,
	}
	return &converted
}

func mapCanonicalEventType(typ schema.CanonicalType) schema.EventType {
	switch typ {
	case schema.CanonicalType("TICKER"):
		return schema.EventTypeTicker
	case schema.CanonicalType("ORDERBOOK.SNAPSHOT"):
		return schema.EventTypeBookSnapshot
	case schema.CanonicalType("ORDERBOOK.DIFF"), schema.CanonicalType("ORDERBOOK.UPDATE"):
		return schema.EventTypeBookUpdate
	case schema.CanonicalType("TRADE"):
		return schema.EventTypeTrade
	case schema.CanonicalType("EXECUTION.REPORT"):
		return schema.EventTypeExecReport
	case schema.CanonicalType("KLINE.SUMMARY"):
		return schema.EventTypeKlineSummary
	default:
		return schema.EventType(typ)
	}
}
