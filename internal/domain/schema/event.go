// Package schema defines canonical event schemas and payload types.
package schema

import (
	"fmt"
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/domain/errs"
)

// RouteType identifies canonical Meltica route identifiers used across control-plane interactions.
type RouteType string

const (
	// RouteTypeAccountBalance designates account balance updates emitted by providers.
	RouteTypeAccountBalance RouteType = "ACCOUNT.BALANCE"
	// RouteTypeOrderbookSnapshot designates full orderbook snapshot streams.
	RouteTypeOrderbookSnapshot RouteType = "ORDERBOOK.SNAPSHOT"
	// RouteTypeTrade designates trade execution streams.
	RouteTypeTrade RouteType = "TRADE"
	// RouteTypeTicker designates ticker summary streams.
	RouteTypeTicker RouteType = "TICKER"
	// RouteTypeExecutionReport designates order execution report streams.
	RouteTypeExecutionReport RouteType = "EXECUTION.REPORT"
	// RouteTypeKlineSummary designates candlestick summary streams.
	RouteTypeKlineSummary RouteType = "KLINE.SUMMARY"
	// RouteTypeInstrumentUpdate designates instrument catalogue refresh notifications.
	RouteTypeInstrumentUpdate RouteType = "INSTRUMENT.UPDATE"
	// RouteTypeRiskControl designates risk control notifications emitted from runtime safeguards.
	RouteTypeRiskControl RouteType = "RISK.CONTROL"
)

var (
	routeToEventType = map[RouteType]EventType{
		RouteTypeAccountBalance:    EventTypeBalanceUpdate,
		RouteTypeOrderbookSnapshot: EventTypeBookSnapshot,
		RouteTypeTrade:             EventTypeTrade,
		RouteTypeTicker:            EventTypeTicker,
		RouteTypeExecutionReport:   EventTypeExecReport,
		RouteTypeKlineSummary:      EventTypeKlineSummary,
		RouteTypeInstrumentUpdate:  EventTypeInstrumentUpdate,
		RouteTypeRiskControl:       EventTypeRiskControl,
	}
	eventTypeToRoutes = map[EventType]RouteType{
		EventTypeBalanceUpdate:    RouteTypeAccountBalance,
		EventTypeBookSnapshot:     RouteTypeOrderbookSnapshot,
		EventTypeTrade:            RouteTypeTrade,
		EventTypeTicker:           RouteTypeTicker,
		EventTypeExecReport:       RouteTypeExecutionReport,
		EventTypeKlineSummary:     RouteTypeKlineSummary,
		EventTypeInstrumentUpdate: RouteTypeInstrumentUpdate,
		EventTypeRiskControl:      RouteTypeRiskControl,
	}
)

// NormalizeRouteType trims spaces and uppercases the provided canonical route.
func NormalizeRouteType(route RouteType) RouteType {
	trimmed := strings.TrimSpace(string(route))
	if trimmed == "" {
		return ""
	}
	return RouteType(strings.ToUpper(trimmed))
}

// Validate ensures the canonical route name adheres to spec.
func (r RouteType) Validate() error {
	normalized := NormalizeRouteType(r)
	if normalized == "" {
		return errs.New("schema/canonical-type", errs.CodeInvalid, errs.WithMessage("canonical type required"))
	}
	if normalized != r {
		return errs.New("schema/canonical-type", errs.CodeInvalid, errs.WithMessage("canonical type must be uppercase alphanumeric"))
	}
	parts := strings.Split(string(normalized), ".")
	for _, part := range parts {
		if part == "" {
			return errs.New("schema/canonical-type", errs.CodeInvalid, errs.WithMessage("empty canonical type segment"))
		}
		for _, ch := range part {
			if ch < 'A' || ch > 'Z' && (ch < '0' || ch > '9') {
				return errs.New("schema/canonical-type", errs.CodeInvalid, errs.WithMessage("canonical type must be uppercase alphanumeric"))
			}
		}
	}
	return nil
}

// EventTypeForRoute resolves the event type associated with a canonical route.
func EventTypeForRoute(route RouteType) (EventType, bool) {
	normalized := NormalizeRouteType(route)
	if normalized == "" {
		return "", false
	}
	evt, ok := routeToEventType[normalized]
	return evt, ok
}

// RoutesForEvent returns the canonical routes that map to the supplied event type.
func RoutesForEvent(evt EventType) []RouteType {
	route, ok := eventTypeToRoutes[evt]
	if !ok {
		return nil
	}
	return []RouteType{route}
}

// PrimaryRouteForEvent returns the preferred canonical route for an event type.
func PrimaryRouteForEvent(evt EventType) (RouteType, bool) {
	route, ok := eventTypeToRoutes[evt]
	return route, ok
}

// RawInstance is a pre-canonicalized payload produced by upstream adapters.
type RawInstance map[string]any

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

// Subscribe represents a control plane command to add a canonical route.
type Subscribe struct {
	Type      RouteType      `json:"type"`
	Filters   map[string]any `json:"filters,omitempty"`
	RequestID string         `json:"requestId,omitempty"`
}

// Unsubscribe represents a control plane command to remove a canonical route.
type Unsubscribe struct {
	Type      RouteType `json:"type"`
	RequestID string    `json:"requestId,omitempty"`
}

// ValidateInstrument verifies the canonical instrument formatting.
func ValidateInstrument(symbol string) error {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return errs.New("schema/instrument", errs.CodeInvalid, errs.WithMessage("instrument required"))
	}
	_, err := validateInstrumentSymbol(symbol)
	return err
}

// BuildEventKey constructs the default idempotency key for an event.
func BuildEventKey(instr string, route RouteType, seq uint64) string {
	return fmt.Sprintf("%s:%s:%d", strings.TrimSpace(instr), NormalizeRouteType(route), seq)
}

// Event represents a canonical event emitted by providers or dispatcher.
type Event struct {
	returned       bool
	EventID        string    `json:"event_id"`
	RoutingVersion int       `json:"routing_version"`
	Provider       string    `json:"provider"`
	Symbol         string    `json:"symbol"`
	Type           EventType `json:"type"`
	SeqProvider    uint64    `json:"seq_provider"`
	IngestTS       time.Time `json:"ingest_ts"`
	EmitTS         time.Time `json:"emit_ts"`
	Payload        any       `json:"payload"`
}

// Reset zeroes the event for pool reuse.
func (e *Event) Reset() {
	if e == nil {
		return
	}
	e.EventID = ""
	e.RoutingVersion = 0
	e.Provider = ""
	e.Symbol = ""
	e.Type = ""
	e.SeqProvider = 0
	e.IngestTS = time.Time{}
	e.EmitTS = time.Time{}
	e.Payload = nil
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

// EventType enumerates canonical event categories.
type EventType string

const (
	// EventTypeBookSnapshot identifies full depth snapshots.
	// NOTE: Adapters MUST always emit full orderbooks, never deltas.
	// Exchange-specific delta handling should be done within the adapter.
	EventTypeBookSnapshot EventType = "BookSnapshot"
	// EventTypeTrade identifies trade executions.
	EventTypeTrade EventType = "Trade"
	// EventTypeTicker identifies ticker summary events.
	EventTypeTicker EventType = "Ticker"
	// EventTypeExecReport identifies order execution reports.
	EventTypeExecReport EventType = "ExecReport"
	// EventTypeKlineSummary identifies candlestick summary events.
	EventTypeKlineSummary EventType = "KlineSummary"
	// EventTypeInstrumentUpdate identifies provider instrument catalogue refresh notifications.
	EventTypeInstrumentUpdate EventType = "InstrumentUpdate"
	// EventTypeBalanceUpdate identifies account balance updates emitted by providers.
	EventTypeBalanceUpdate EventType = "BalanceUpdate"
	// EventTypeRiskControl identifies risk control notifications emitted by runtime safeguards.
	EventTypeRiskControl EventType = "RiskControl"
)

// PriceLevel describes an order book price level using decimal strings.
type PriceLevel struct {
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
}

// BookSnapshotPayload conveys a full snapshot of order book depth.
// Adapters MUST always send the complete orderbook state, not deltas.
// Any delta-based exchanges should maintain the full book state within the adapter.
type BookSnapshotPayload struct {
	Bids       []PriceLevel `json:"bids"`
	Asks       []PriceLevel `json:"asks"`
	Checksum   string       `json:"checksum"`
	LastUpdate time.Time    `json:"last_update"`

	// Binance-specific sequence tracking (optional, used internally during assembly)
	FirstUpdateID uint64 `json:"first_update_id,omitempty"` // U - First update ID in event
	FinalUpdateID uint64 `json:"final_update_id,omitempty"` // u - Final update ID in event
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
	ClientOrderID    string          `json:"client_order_id"`
	ExchangeOrderID  string          `json:"exchange_order_id"`
	State            ExecReportState `json:"state"`
	Side             TradeSide       `json:"side"`
	OrderType        OrderType       `json:"order_type"`
	Price            string          `json:"price"`
	Quantity         string          `json:"quantity"`
	FilledQuantity   string          `json:"filled_quantity"`
	RemainingQty     string          `json:"remaining_qty"`
	AvgFillPrice     string          `json:"avg_fill_price"`
	CommissionAmount string          `json:"commission_amount,omitempty"`
	CommissionAsset  string          `json:"commission_asset,omitempty"`
	Timestamp        time.Time       `json:"timestamp"`
	RejectReason     *string         `json:"reject_reason,omitempty"`
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

// InstrumentUpdatePayload advertises an updated instrument definition for a provider.
type InstrumentUpdatePayload struct {
	Instrument Instrument `json:"instrument"`
}

// BalanceUpdatePayload reports the current account balance for a given currency.
type BalanceUpdatePayload struct {
	Currency  string    `json:"currency"`
	Total     string    `json:"total"`
	Available string    `json:"available"`
	Timestamp time.Time `json:"timestamp"`
}

// RiskControlStatus enumerates state transitions for risk notifications.
type RiskControlStatus string

const (
	// RiskControlStatusTriggered indicates a risk breach preventing order submission.
	RiskControlStatusTriggered RiskControlStatus = "TRIGGERED"
	// RiskControlStatusCleared indicates risk controls have been reset or cleared.
	RiskControlStatusCleared RiskControlStatus = "CLEARED"
)

// RiskControlPayload conveys details about runtime risk control actions.
type RiskControlPayload struct {
	StrategyID         string            `json:"strategy_id"`
	Provider           string            `json:"provider"`
	Symbol             string            `json:"symbol"`
	Status             RiskControlStatus `json:"status"`
	Reason             string            `json:"reason"`
	BreachType         string            `json:"breach_type"`
	Metrics            map[string]string `json:"metrics,omitempty"`
	KillSwitchEngaged  bool              `json:"kill_switch_engaged"`
	CircuitBreakerOpen bool              `json:"circuit_breaker_open"`
	Timestamp          time.Time         `json:"timestamp"`
}
