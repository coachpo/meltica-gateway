// Package schema defines canonical event schemas and payload types.
package schema

import (
	"fmt"
	"strings"
	"time"

	"github.com/coachpo/meltica/errs"
)

// CanonicalType identifies canonical Meltica event categories (e.g. TICKER, ORDERBOOK.SNAPSHOT).
type CanonicalType string

// MelticaEvent represents a canonical event emitted by the dispatcher or conductor.
type MelticaEvent struct {
	Type           CanonicalType
	Source         string
	Ts             time.Time
	Instrument     string
	Market         string
	Seq            uint64
	Key            string
	Payload        any
	Latency        time.Duration
	TraceID        string
	RoutingVersion int
}

// RawInstance is the pre-canonicalized payload produced by upstream adapters.
type RawInstance map[string]any

// Subscribe represents a control plane command to add a canonical route.
type Subscribe struct {
	Type      CanonicalType  `json:"type"`
	Filters   map[string]any `json:"filters,omitempty"`
	TraceID   string         `json:"traceId,omitempty"`
	RequestID string         `json:"requestId,omitempty"`
}

// Unsubscribe represents a control plane command to remove a canonical route.
type Unsubscribe struct {
	Type      CanonicalType `json:"type"`
	TraceID   string        `json:"traceId,omitempty"`
	RequestID string        `json:"requestId,omitempty"`
}

// Command identifies a control bus command envelope.
type Command interface {
	commandKind() CommandKind
}

// CommandKind identifies command type names.
type CommandKind string

const (
	// CommandKindSubscribe represents a subscribe request.
	CommandKindSubscribe CommandKind = "subscribe"
	// CommandKindUnsubscribe represents an unsubscribe request.
	CommandKindUnsubscribe CommandKind = "unsubscribe"
)

func (Subscribe) commandKind() CommandKind   { return CommandKindSubscribe }
func (Unsubscribe) commandKind() CommandKind { return CommandKindUnsubscribe }

// Validate ensures the canonical type adheres to spec.
func (c CanonicalType) Validate() error {
	if c == "" {
		return errs.New("schema/canonical-type", errs.CodeInvalid, errs.WithMessage("canonical type required"))
	}
	parts := strings.Split(string(c), ".")
	for _, part := range parts {
		if part == "" {
			return errs.New("schema/canonical-type", errs.CodeInvalid, errs.WithMessage("empty canonical type segment"))
		}
		for _, r := range part {
			if r < 'A' || r > 'Z' && (r < '0' || r > '9') {
				return errs.New("schema/canonical-type", errs.CodeInvalid, errs.WithMessage("canonical type must be uppercase alphanumeric"))
			}
		}
	}
	return nil
}

// ValidateInstrument verifies the canonical instrument representation (BASE-QUOTE).
func ValidateInstrument(symbol string) error {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return errs.New("schema/instrument", errs.CodeInvalid, errs.WithMessage("instrument required"))
	}
	if !strings.Contains(symbol, "-") {
		return errs.New("schema/instrument", errs.CodeInvalid, errs.WithMessage("instrument must contain '-'"))
	}
	parts := strings.Split(symbol, "-")
	if len(parts) != 2 {
		return errs.New("schema/instrument", errs.CodeInvalid, errs.WithMessage("instrument requires base-quote"))
	}
	for _, part := range parts {
		if part == "" {
			return errs.New("schema/instrument", errs.CodeInvalid, errs.WithMessage("instrument contains empty leg"))
		}
		if strings.ToUpper(part) != part {
			return errs.New("schema/instrument", errs.CodeInvalid, errs.WithMessage("instrument must be uppercase"))
		}
	}
	return nil
}

// BuildEventKey constructs the default idempotency key for an event.
func BuildEventKey(instr string, typ CanonicalType, seq uint64) string {
	return fmt.Sprintf("%s:%s:%d", strings.TrimSpace(instr), string(typ), seq)
}

// Clone returns a deep copy of the raw instance.
func (r RawInstance) Clone() RawInstance {
	if len(r) == 0 {
		return RawInstance{}
	}
	out := make(RawInstance, len(r))
	for k, v := range r {
		out[k] = v
	}
	return out
}

// Event represents a canonical event emitted by providers, orchestrator, or dispatcher.
type Event struct {
	returned       bool
	EventID        string    `json:"event_id"`
	MergeID        *string   `json:"merge_id,omitempty"`
	RoutingVersion int       `json:"routing_version"`
	Provider       string    `json:"provider"`
	Symbol         string    `json:"symbol"`
	Type           EventType `json:"type"`
	SeqProvider    uint64    `json:"seq_provider"`
	IngestTS       time.Time `json:"ingest_ts"`
	EmitTS         time.Time `json:"emit_ts"`
	Payload        any       `json:"payload"`
	TraceID        string    `json:"trace_id,omitempty"`
}

// Reset zeroes the event for pool reuse.
func (e *Event) Reset() {
	if e == nil {
		return
	}
	e.EventID = ""
	e.MergeID = nil
	e.RoutingVersion = 0
	e.Provider = ""
	e.Symbol = ""
	e.Type = ""
	e.SeqProvider = 0
	e.IngestTS = time.Time{}
	e.EmitTS = time.Time{}
	e.Payload = nil
	e.TraceID = ""
	e.returned = false
}

// SetReturned toggles the ownership flag for pooling.
func (e *Event) SetReturned(flag bool) {
	if e == nil {
		return
	}
	e.returned = flag
}

// IsReturned reports whether the event currently resides in a pool.
func (e *Event) IsReturned() bool {
	if e == nil {
		return false
	}
	return e.returned
}

// CanonicalEvent is an alias for Event, representing canonical events in pooling contexts.
type CanonicalEvent = Event

// EventType enumerates canonical event categories.
type EventType string

const (
	// EventTypeBookSnapshot identifies full depth snapshots.
	EventTypeBookSnapshot EventType = "BookSnapshot"
	// EventTypeBookUpdate identifies incremental depth updates.
	EventTypeBookUpdate EventType = "BookUpdate"
	// EventTypeTrade identifies trade executions.
	EventTypeTrade EventType = "Trade"
	// EventTypeTicker identifies ticker summary events.
	EventTypeTicker EventType = "Ticker"
	// EventTypeExecReport identifies order execution reports.
	EventTypeExecReport EventType = "ExecReport"
	// EventTypeKlineSummary identifies candlestick summary events.
	EventTypeKlineSummary EventType = "KlineSummary"
)

// Coalescable reports whether an event type can be coalesced under backpressure.
func (et EventType) Coalescable() bool {
	switch et {
	case EventTypeTicker, EventTypeBookUpdate, EventTypeKlineSummary:
		return true
	case EventTypeBookSnapshot, EventTypeTrade, EventTypeExecReport:
		return false
	default:
		return false
	}
}

// PriceLevel describes an order book price level using decimal strings.
type PriceLevel struct {
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
}

// BookSnapshotPayload conveys a full snapshot of order book depth.
type BookSnapshotPayload struct {
	Bids       []PriceLevel `json:"bids"`
	Asks       []PriceLevel `json:"asks"`
	Checksum   string       `json:"checksum"`
	LastUpdate time.Time    `json:"last_update"`
}

// BookUpdateType differentiates delta vs full-book updates.
type BookUpdateType string

const (
	// BookUpdateTypeDelta represents delta updates.
	BookUpdateTypeDelta BookUpdateType = "Delta"
	// BookUpdateTypeFull represents full book refreshes.
	BookUpdateTypeFull BookUpdateType = "Full"
)

// BookUpdatePayload carries incremental or full updates to the order book.
type BookUpdatePayload struct {
	UpdateType BookUpdateType `json:"update_type"`
	Bids       []PriceLevel   `json:"bids"`
	Asks       []PriceLevel   `json:"asks"`
	Checksum   string         `json:"checksum"`
}

// TradeSide captures the direction of a trade.
type TradeSide string

const (
	// TradeSideBuy indicates buy side fills.
	TradeSideBuy TradeSide = "Buy"
	// TradeSideSell indicates sell side fills.
	TradeSideSell TradeSide = "Sell"
)

// TradePayload represents an executed trade event.
type TradePayload struct {
	TradeID   string    `json:"trade_id"`
	Side      TradeSide `json:"side"`
	Price     string    `json:"price"`
	Quantity  string    `json:"quantity"`
	Timestamp time.Time `json:"timestamp"`
}

// TickerPayload conveys ticker statistics.
type TickerPayload struct {
	LastPrice string    `json:"last_price"`
	BidPrice  string    `json:"bid_price"`
	AskPrice  string    `json:"ask_price"`
	Volume24h string    `json:"volume_24h"`
	Timestamp time.Time `json:"timestamp"`
}

// ExecReportState enumerates order lifecycle states.
type ExecReportState string

const (
	// ExecReportStateACK indicates acknowledgement.
	ExecReportStateACK ExecReportState = "ACK"
	// ExecReportStatePARTIAL indicates partial fill.
	ExecReportStatePARTIAL ExecReportState = "PARTIAL"
	// ExecReportStateFILLED indicates full fill.
	ExecReportStateFILLED ExecReportState = "FILLED"
	// ExecReportStateCANCELLED indicates cancellation.
	ExecReportStateCANCELLED ExecReportState = "CANCELLED"
	// ExecReportStateREJECTED indicates rejection.
	ExecReportStateREJECTED ExecReportState = "REJECTED"
	// ExecReportStateEXPIRED indicates expiry.
	ExecReportStateEXPIRED ExecReportState = "EXPIRED"
)

// OrderType enumerates order types supported in execution reports.
type OrderType string

const (
	// OrderTypeLimit represents limit orders.
	OrderTypeLimit OrderType = "Limit"
	// OrderTypeMarket represents market orders.
	OrderTypeMarket OrderType = "Market"
)

// ExecReportPayload represents state transitions for submitted orders.
type ExecReportPayload struct {
	ClientOrderID   string          `json:"client_order_id"`
	ExchangeOrderID string          `json:"exchange_order_id"`
	State           ExecReportState `json:"state"`
	Side            TradeSide       `json:"side"`
	OrderType       OrderType       `json:"order_type"`
	Price           string          `json:"price"`
	Quantity        string          `json:"quantity"`
	FilledQuantity  string          `json:"filled_quantity"`
	RemainingQty    string          `json:"remaining_qty"`
	AvgFillPrice    string          `json:"avg_fill_price"`
	Timestamp       time.Time       `json:"timestamp"`
	RejectReason    *string         `json:"reject_reason,omitempty"`
}

// KlineSummaryPayload represents aggregated candlestick data.
type KlineSummaryPayload struct {
	OpenPrice  string    `json:"open_price"`
	ClosePrice string    `json:"close_price"`
	HighPrice  string    `json:"high_price"`
	LowPrice   string    `json:"low_price"`
	Volume     string    `json:"volume"`
	OpenTime   time.Time `json:"open_time"`
	CloseTime  time.Time `json:"close_time"`
}
