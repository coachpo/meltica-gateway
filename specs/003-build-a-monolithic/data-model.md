# Data Model: Monolithic Auto-Trading Application

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)  
**Date**: October 12, 2025  
**Status**: Complete

## Overview

This document defines the canonical data structures, state machines, and relationships for the monolithic auto-trading application. All structures are immutable where possible and designed for thread-safe concurrent access.

---

## 1. Canonical Event Types

### 1.1 Base Event

All canonical events extend this base structure.

```go
type Event struct {
    EventID       string    `json:"event_id"`        // Unique identifier: {provider}:{symbol}:{type}:{timestamp}:{seq}
    MergeID       *string   `json:"merge_id"`        // Set if part of merged event
    RoutingVersion int      `json:"routing_version"` // Incremented on routing table updates
    Provider      string    `json:"provider"`        // e.g., "binance", "coinbase"
    Symbol        string    `json:"symbol"`          // Canonical format: "BTC-USDT"
    Type          EventType `json:"type"`            // BookSnapshot, BookUpdate, Trade, Ticker, ExecReport
    SeqProvider   uint64    `json:"seq_provider"`    // Provider's sequence number for ordering
    IngestTS      time.Time `json:"ingest_ts"`       // Timestamp when ingested by provider adapter
    EmitTS        time.Time `json:"emit_ts"`         // Timestamp when emitted to orchestrator
    Payload       any       `json:"payload"`         // Type-specific payload (BookUpdatePayload, TradePayload, etc.)
}

type EventType string

const (
    EventTypeBookSnapshot EventType = "BookSnapshot"
    EventTypeBookUpdate   EventType = "BookUpdate"
    EventTypeTrade        EventType = "Trade"
    EventTypeTicker       EventType = "Ticker"
    EventTypeExecReport   EventType = "ExecReport"
)

// Coalescable returns true if this event type can be coalesced under backpressure
func (et EventType) Coalescable() bool {
    return et == EventTypeTicker || et == EventTypeBookUpdate || et == "KlineSummary"
}
```

### 1.2 BookSnapshot

```go
type BookSnapshotPayload struct {
    Bids       []PriceLevel `json:"bids"`        // Sorted descending by price
    Asks       []PriceLevel `json:"asks"`        // Sorted ascending by price
    Checksum   string       `json:"checksum"`    // Provider-specific checksum
    LastUpdate time.Time    `json:"last_update"` // Provider's last update timestamp
}

type PriceLevel struct {
    Price    string `json:"price"`    // Decimal string (e.g., "50000.50")
    Quantity string `json:"quantity"` // Decimal string (e.g., "1.25")
}
```

**Invariants**:
- Bids sorted descending (highest price first)
- Asks sorted ascending (lowest price first)
- Price and quantity are decimal strings (no floating-point)

### 1.3 BookUpdate

```go
type BookUpdatePayload struct {
    UpdateType BookUpdateType `json:"update_type"` // Delta or Full
    Bids       []PriceLevel   `json:"bids"`        // Updates to bid side
    Asks       []PriceLevel   `json:"asks"`        // Updates to ask side
    Checksum   string         `json:"checksum"`    // Provider-specific checksum
}

type BookUpdateType string

const (
    BookUpdateTypeDelta BookUpdateType = "Delta" // Incremental update
    BookUpdateTypeFull  BookUpdateType = "Full"  // Full book replacement
)
```

**Semantics**:
- `Delta`: Apply price level changes (quantity=0 means remove level)
- `Full`: Replace entire order book

### 1.4 Trade

```go
type TradePayload struct {
    TradeID   string    `json:"trade_id"`   // Provider-specific trade ID
    Side      TradeSide `json:"side"`       // Buy or Sell
    Price     string    `json:"price"`      // Execution price (decimal string)
    Quantity  string    `json:"quantity"`   // Executed quantity (decimal string)
    Timestamp time.Time `json:"timestamp"`  // Execution timestamp from provider
}

type TradeSide string

const (
    TradeSideBuy  TradeSide = "Buy"
    TradeSideSell TradeSide = "Sell"
)
```

### 1.5 Ticker

```go
type TickerPayload struct {
    LastPrice   string    `json:"last_price"`   // Most recent trade price
    BidPrice    string    `json:"bid_price"`    // Best bid
    AskPrice    string    `json:"ask_price"`    // Best ask
    Volume24h   string    `json:"volume_24h"`   // 24-hour volume
    Timestamp   time.Time `json:"timestamp"`    // Provider timestamp
}
```

### 1.6 ExecReport (Execution Report)

```go
type ExecReportPayload struct {
    ClientOrderID  string          `json:"client_order_id"` // Client-assigned order ID (idempotency key)
    ExchangeOrderID string         `json:"exchange_order_id"` // Provider-assigned order ID
    State          ExecReportState `json:"state"`           // Order lifecycle state
    Side           TradeSide       `json:"side"`            // Buy or Sell
    OrderType      OrderType       `json:"order_type"`      // Limit or Market
    Price          string          `json:"price"`           // Order price (limit only)
    Quantity       string          `json:"quantity"`        // Original quantity
    FilledQuantity string          `json:"filled_quantity"` // Cumulative filled quantity
    RemainingQty   string          `json:"remaining_qty"`   // Remaining open quantity
    AvgFillPrice   string          `json:"avg_fill_price"`  // Average execution price
    Timestamp      time.Time       `json:"timestamp"`       // State transition timestamp
    RejectReason   *string         `json:"reject_reason"`   // Set if state=REJECTED
}

type ExecReportState string

const (
    ExecReportStateACK       ExecReportState = "ACK"       // Order acknowledged
    ExecReportStatePARTIAL   ExecReportState = "PARTIAL"   // Partially filled
    ExecReportStateFILLED    ExecReportState = "FILLED"    // Fully filled
    ExecReportStateCANCELLED ExecReportState = "CANCELLED" // Cancelled by user or exchange
    ExecReportStateREJECTED  ExecReportState = "REJECTED"  // Rejected by exchange
    ExecReportStateEXPIRED   ExecReportState = "EXPIRED"   // Expired (time-in-force)
)

type OrderType string

const (
    OrderTypeLimit  OrderType = "Limit"
    OrderTypeMarket OrderType = "Market"
)
```

**Invariants**:
- ExecReport events are NEVER coalesced or dropped
- `filled_quantity + remaining_qty = quantity` (always)
- State transitions are monotonic: ACK → PARTIAL* → (FILLED | CANCELLED | EXPIRED)

---

## 2. Merge Window State

Tracks multi-provider event windowing for merged subscriptions.

```go
type MergeWindow struct {
    MergeID          string             `json:"merge_id"`          // Unique window ID
    MergeKey         MergeKey           `json:"merge_key"`         // (symbol, eventType)
    OpenTime         time.Time          `json:"open_time"`         // Window opened timestamp
    CloseTime        *time.Time         `json:"close_time"`        // Window closed timestamp (nil if open)
    ExpectedProviders []string          `json:"expected_providers"` // Providers configured for this merge
    ReceivedEvents   map[string]*Event  `json:"received_events"`   // Provider → Event
    EventCount       int                `json:"event_count"`       // Total events received
    Status           MergeWindowStatus  `json:"status"`            // Open, Closed, Suppressed
    MaxEvents        int                `json:"max_events"`        // Close threshold (e.g., 1000)
    WindowDuration   time.Duration      `json:"window_duration"`   // Time threshold (e.g., 10s)
}

type MergeKey struct {
    Symbol    string    `json:"symbol"`     // e.g., "BTC-USDT"
    EventType EventType `json:"event_type"` // e.g., "BookSnapshot"
}

type MergeWindowStatus string

const (
    MergeWindowStatusOpen       MergeWindowStatus = "Open"       // Window is collecting events
    MergeWindowStatusClosed     MergeWindowStatus = "Closed"     // Window closed and emitted
    MergeWindowStatusSuppressed MergeWindowStatus = "Suppressed" // Window closed but suppressed (partial)
)
```

**State Machine**:
1. **Open**: Window created on first event arrival
2. **Closed**: Window closes when `time >= openTime + windowDuration` OR `eventCount >= maxEvents`
3. **Suppressed**: Window closes but not all expected providers present → suppress (no emit)
4. **Emitted**: Window closes with all expected providers → merge events and emit with merge_id

**Closure Conditions**:
- **Time-based**: `time.Now() >= window.OpenTime.Add(window.WindowDuration)`
- **Count-based**: `window.EventCount >= window.MaxEvents`
- **Completeness check**: `len(window.ReceivedEvents) == len(window.ExpectedProviders)`

---

## 3. Stream Ordering State

Manages per-stream event reordering and buffering.

```go
type StreamKey struct {
    Provider  string    `json:"provider"`   // e.g., "binance"
    Symbol    string    `json:"symbol"`     // e.g., "BTC-USDT"
    EventType EventType `json:"event_type"` // e.g., "Trade"
}

type StreamBuffer struct {
    StreamKey    StreamKey          `json:"stream_key"`
    EventHeap    *EventHeap         `json:"-"`              // Priority queue sorted by seq_provider
    LastEmitted  uint64             `json:"last_emitted"`   // seq_provider of last emitted event
    FlushTimer   *time.Timer        `json:"-"`              // Flush buffer every 50ms
    MaxBufferSize int               `json:"max_buffer_size"` // Default: 200
    LatenceTolerance time.Duration  `json:"latency_tolerance"` // Default: 150ms
}

// EventHeap implements heap.Interface for seq_provider ordering
type EventHeap []*Event

func (h EventHeap) Len() int { return len(h) }
func (h EventHeap) Less(i, j int) bool {
    // Primary sort: seq_provider (ascending)
    if h[i].SeqProvider != h[j].SeqProvider {
        return h[i].SeqProvider < h[j].SeqProvider
    }
    // Fallback: ingest_ts (ascending)
    return h[i].IngestTS.Before(h[j].IngestTS)
}
func (h EventHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *EventHeap) Push(x any)   { *h = append(*h, x.(*Event)) }
func (h *EventHeap) Pop() any {
    old := *h
    n := len(old)
    x := old[n-1]
    *h = old[0 : n-1]
    return x
}
```

**Ordering Logic**:
1. Events arrive with `seq_provider` and `ingest_ts`
2. Insert into priority heap (O(log n))
3. On flush (every 50ms) or buffer full: pop events with `seq_provider <= lastEmitted + latenceTolerance`
4. Update `lastEmitted` to highest seq_provider emitted
5. Drop events with `seq_provider > lastEmitted + maxBufferSize` (too late)

---

## 4. Subscription and Routing

### 4.1 Subscription

```go
type Subscription struct {
    SubscriptionID string            `json:"subscription_id"` // Unique ID
    ConsumerID     string            `json:"consumer_id"`     // Consumer identifier
    Symbol         string            `json:"symbol"`          // e.g., "BTC-USDT"
    Providers      []string          `json:"providers"`       // ["binance"] or ["binance", "coinbase"]
    Merged         bool              `json:"merged"`          // If true, expect merged events
    Active         bool              `json:"active"`          // Subscription state
    CreatedAt      time.Time         `json:"created_at"`
    UpdatedAt      time.Time         `json:"updated_at"`
}
```

### 4.2 Routing Table

```go
type RoutingTable struct {
    Version       int                         `json:"version"`        // Incremented on updates
    Subscriptions map[string]*Subscription    `json:"subscriptions"`  // subscriptionID → Subscription
    RouteIndex    map[RouteKey][]string       `json:"route_index"`    // (symbol, provider) → []consumerID
    mu            sync.RWMutex                `json:"-"`
}

type RouteKey struct {
    Symbol   string `json:"symbol"`
    Provider string `json:"provider"`
}

// LookupConsumers returns consumer IDs subscribed to (symbol, provider)
func (rt *RoutingTable) LookupConsumers(symbol, provider string) []string {
    rt.mu.RLock()
    defer rt.mu.RUnlock()
    key := RouteKey{Symbol: symbol, Provider: provider}
    return rt.RouteIndex[key]
}
```

**Updates**:
- Subscribe: Add entry to `Subscriptions` and update `RouteIndex`
- Unsubscribe: Mark `Active=false` and remove from `RouteIndex`
- Version incremented on every update → carried in `event.RoutingVersion`

---

## 5. Order and Order Request

### 5.1 Order Request

```go
type OrderRequest struct {
    ClientOrderID string    `json:"client_order_id"` // MUST be unique per consumer (idempotency key)
    ConsumerID    string    `json:"consumer_id"`
    Provider      string    `json:"provider"`        // Target exchange
    Symbol        string    `json:"symbol"`          // e.g., "BTC-USDT"
    Side          TradeSide `json:"side"`            // Buy or Sell
    OrderType     OrderType `json:"order_type"`      // Limit or Market
    Price         *string   `json:"price"`           // Required for Limit, nil for Market
    Quantity      string    `json:"quantity"`        // Order size
    Timestamp     time.Time `json:"timestamp"`       // Request timestamp
}
```

**Idempotency**:
- Dispatcher maintains in-memory map: `map[ClientOrderID]bool` (last 1 hour)
- Duplicate `ClientOrderID` within window → reject with error (no submission to provider)
- Provider adapters also track `ClientOrderID` to ensure exactly-once submission

### 5.2 Order State (Internal Tracking)

```go
type Order struct {
    ClientOrderID   string          `json:"client_order_id"`
    ExchangeOrderID string          `json:"exchange_order_id"` // Set after ACK
    ConsumerID      string          `json:"consumer_id"`
    Provider        string          `json:"provider"`
    Symbol          string          `json:"symbol"`
    Side            TradeSide       `json:"side"`
    OrderType       OrderType       `json:"order_type"`
    Price           *string         `json:"price"`
    Quantity        string          `json:"quantity"`
    FilledQuantity  string          `json:"filled_quantity"`
    State           ExecReportState `json:"state"`           // Current state
    CreatedAt       time.Time       `json:"created_at"`
    UpdatedAt       time.Time       `json:"updated_at"`
}
```

**Lifecycle**:
1. Consumer submits `OrderRequest` → Dispatcher creates `Order` with `State=PENDING`
2. Dispatcher routes to provider adapter → Provider submits to exchange
3. Exchange ACK → ExecReport with `State=ACK` → Update `Order.State=ACK`, set `ExchangeOrderID`
4. Partial fills → ExecReport with `State=PARTIAL` → Update `FilledQuantity`
5. Terminal states (FILLED, CANCELLED, REJECTED, EXPIRED) → Mark order complete

---

## 6. Consumer and Trading Switch

### 6.1 Consumer

```go
type Consumer struct {
    ConsumerID       string               `json:"consumer_id"`       // Unique identifier
    Name             string               `json:"name"`              // Human-readable name
    TradingSwitchEnabled bool            `json:"trading_switch_enabled"` // Enable/disable trading
    Subscriptions    []*Subscription      `json:"subscriptions"`     // Active subscriptions
    DataBusChan      chan *Event          `json:"-"`                 // Channel for receiving events
    ControlBusChan   chan *ControlMessage `json:"-"`                 // Channel for control messages
}
```

### 6.2 Trading Switch

```go
type TradingSwitch struct {
    ConsumerID string `json:"consumer_id"`
    Enabled    bool   `json:"enabled"`    // If false, suppress all order submissions
    UpdatedAt  time.Time `json:"updated_at"`
}

// CheckEnabled returns true if trading is enabled for this consumer
func (ts *TradingSwitch) CheckEnabled() bool {
    return ts.Enabled
}
```

**Enforcement**:
- Consumer checks `TradingSwitch.Enabled` before submitting `OrderRequest`
- If disabled → suppress locally (no submission to dispatcher)
- Control Bus command `SET_TRADING_MODE` updates switch state

---

## 7. Control Messages

```go
type ControlMessage struct {
    MessageID   string             `json:"message_id"`
    ConsumerID  string             `json:"consumer_id"`
    Type        ControlMessageType `json:"type"`
    Payload     any                `json:"payload"` // Type-specific payload
    Timestamp   time.Time          `json:"timestamp"`
}

type ControlMessageType string

const (
    ControlMessageTypeSubscribe       ControlMessageType = "Subscribe"
    ControlMessageTypeUnsubscribe     ControlMessageType = "Unsubscribe"
    ControlMessageTypeMergedSubscribe ControlMessageType = "MergedSubscribe"
    ControlMessageTypeSetTradingMode  ControlMessageType = "SetTradingMode"
)

// Subscribe Payload
type SubscribePayload struct {
    Symbol    string   `json:"symbol"`
    Providers []string `json:"providers"`
}

// MergedSubscribe Payload
type MergedSubscribePayload struct {
    Symbol    string   `json:"symbol"`
    Providers []string `json:"providers"` // Must have 2+ providers
}

// SetTradingMode Payload
type SetTradingModePayload struct {
    Enabled bool `json:"enabled"`
}
```

---

## 8. Telemetry and Metrics

### 8.1 Telemetry Event

```go
type TelemetryEvent struct {
    EventID    string            `json:"event_id"`
    Type       TelemetryEventType `json:"type"`
    Severity   Severity          `json:"severity"`
    Timestamp  time.Time         `json:"timestamp"`
    TraceID    string            `json:"trace_id"`    // Distributed trace ID
    DecisionID string            `json:"decision_id"` // Decision point ID
    Metadata   map[string]any    `json:"metadata"`    // Type-specific metadata
}

type TelemetryEventType string

const (
    TelemetryEventTypeBookResync          TelemetryEventType = "book.resync"
    TelemetryEventTypeMergeSuppressed     TelemetryEventType = "merge.suppressed_partial"
    TelemetryEventTypeLateEventDropped    TelemetryEventType = "late_event.dropped"
    TelemetryEventTypeCoalescingApplied   TelemetryEventType = "coalescing.applied"
    TelemetryEventTypeBackpressureTriggered TelemetryEventType = "backpressure.triggered"
)

type Severity string

const (
    SeverityInfo  Severity = "INFO"
    SeverityWarn  Severity = "WARN"
    SeverityError Severity = "ERROR"
)
```

### 8.2 Metrics

```go
type Metrics struct {
    // Dispatcher
    BufferDepthPerStream    map[StreamKey]int          // Current buffer size per stream
    CoalescedDropsTotal     int64                      // Total coalescable events dropped
    ThrottledMilliseconds   int64                      // Total time spent throttled
    SuppressedPartialsTotal int64                      // Total partial windows suppressed
    
    // Provider
    BookResyncsTotal        map[string]int64           // Provider → resync count
    ChecksumFailuresTotal   map[string]int64           // Provider → checksum failure count
    WebSocketReconnects     map[string]int64           // Provider → reconnect count
    
    // Orchestrator
    MergeWindowsOpened      int64                      // Total windows opened
    MergeWindowsClosed      int64                      // Total windows closed
    LateFragmentsDropped    int64                      // Total late fragments dropped
}
```

---

## Entity Relationships

```
Consumer 1─────* Subscription
    │
    └──────1 TradingSwitch

Subscription *─────* RoutingTable

Provider ──────> Orchestrator ──────> Dispatcher ──────> DataBus ──────> Consumer
    │                │                      │
    └──> BookAssembler  └──> MergeWindow   └──> StreamBuffer
    │                                       │
    └──> WebSocketClient                   └──> Coalescer
    │
    └──> RESTClient

Order ──────1 ExecReport *
    │
    └─────> OrderRequest

TelemetryEvent ───> TelemetryBus (ops-only)
```

---

## Validation Rules

### Event Validation
- `EventID` MUST be unique (enforced by dispatcher deduplication)
- `SeqProvider` MUST be monotonically increasing within a provider stream
- `Symbol` MUST be in canonical format: `BASE-QUOTE` (e.g., "BTC-USDT")
- `Price` and `Quantity` fields MUST be decimal strings (no floats)

### Order Validation
- `ClientOrderID` MUST be unique per consumer
- `Quantity` MUST be positive decimal string
- `Price` MUST be positive decimal string (if OrderType=Limit)
- `Side` MUST be Buy or Sell
- `OrderType` MUST be Limit or Market

### Merge Window Validation
- `ExpectedProviders` MUST have 2+ providers for merged subscriptions
- `WindowDuration` MUST be positive (default: 10 seconds)
- `MaxEvents` MUST be positive (default: 1000)

---

## Summary

This data model provides:
- **Type safety**: Strong typing for all events, orders, and control messages
- **Immutability**: Events are immutable after creation; state updates create new objects
- **Thread safety**: Routing table and buffers use mutexes for concurrent access
- **Traceability**: TraceID and DecisionID propagation for observability
- **Idempotency**: ClientOrderID for orders, EventID for events
- **Lossless execution path**: ExecReport never coalesced or dropped

All structures are designed for deterministic testing and race-free concurrent operation.

