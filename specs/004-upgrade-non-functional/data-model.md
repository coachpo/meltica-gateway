# Data Model: Performance & Memory Architecture Upgrade

**Feature**: 004-upgrade-non-functional  
**Phase**: 1 (Design & Contracts)  
**Date**: 2025-10-13

## Overview

This document defines the pooled struct types, pool management entities, and lifecycle state machines for the performance and memory architecture upgrade. Six hot-path struct types are converted to poolable objects with Reset() methods. Pool manager coordinates Get/Put operations with bounded capacity, timeout handling, and double-Put() detection.

## Core Pooled Entities

### 1. WsFrame (WebSocket Frame Envelope)

**Purpose**: Raw WebSocket frame data received from provider connections

**Lifecycle**: Get() on frame receipt → Parse into ProviderRaw → Put() after parsing

**Fields**:
```go
type WsFrame struct {
    // Ownership tracking (for double-Put detection)
    returned bool  // Internal: true if Put() called
    
    // Frame metadata
    Provider    string    // Provider name (e.g., "binance")
    ConnID      string    // Connection identifier
    ReceivedAt  int64     // Unix timestamp (nanoseconds)
    
    // Frame payload
    MessageType int       // WebSocket message type (text/binary)
    Data        []byte    // Raw frame bytes
}
```

**Reset() Behavior**:
- Zero all fields: `Provider = ""`, `ConnID = ""`, `ReceivedAt = 0`, `MessageType = 0`
- Release slice: `Data = nil` (allow GC to reclaim backing array)
- Clear flag: `returned = false`

**Validation Rules**:
- `Provider` must be non-empty when in use
- `Data` must be non-nil when in use
- `ReceivedAt` must be > 0 when in use

---

### 2. ProviderRaw (Exchange-Specific Payload)

**Purpose**: Exchange-specific raw event payload before normalization

**Lifecycle**: Get() after WS frame parse → Normalize to CanonicalEvent → Put() after normalization

**Fields**:
```go
type ProviderRaw struct {
    // Ownership tracking
    returned bool
    
    // Event metadata
    Provider   string           // Provider name (e.g., "binance")
    StreamName string           // Stream identifier (e.g., "btcusdt@trade")
    ReceivedAt int64            // Unix timestamp (nanoseconds)
    
    // Raw payload
    Payload    json.RawMessage  // Unparsed JSON from provider
}
```

**Reset() Behavior**:
- Zero fields: `Provider = ""`, `StreamName = ""`, `ReceivedAt = 0`
- Release slice: `Payload = nil`
- Clear flag: `returned = false`

**Validation Rules**:
- `Provider` must be non-empty
- `Payload` must be valid JSON (checked during normalization, not pooling)

---

### 3. CanonicalEvent (Normalized Meltica Event)

**Purpose**: Normalized event in Meltica canonical schema

**Lifecycle**: Get() during normalization → Route through Dispatcher → Put() after fan-out

**Fields**:
```go
type CanonicalEvent struct {
    // Ownership tracking
    returned bool
    
    // Event identity
    Provider   string  // Provider name (e.g., "binance")
    Symbol     string  // Canonical symbol (BASE-QUOTE, e.g., "BTC-USDT")
    EventType  int     // Event type enum (Trade=1, BookUpdate=2, Ticker=3, etc.)
    
    // Sequencing
    SeqProvider int64  // Provider-assigned sequence number (if available)
    IngestTS    int64  // Ingestion timestamp (nanoseconds)
    ExchangeTS  int64  // Exchange timestamp (nanoseconds, if available)
    
    // Payload
    Data       []byte         // Serialized event payload (JSON)
    Metadata   map[string]string  // Optional metadata (version, checksum, etc.)
    
    // Observability
    TraceID    string  // Trace identifier for ops debugging
    DecisionID string  // Decision identifier for order correlation
}
```

**Reset() Behavior**:
- Zero all scalar fields
- Release slice: `Data = nil`
- Release map: `Metadata = nil`
- Clear strings: `TraceID = ""`, `DecisionID = ""`
- Clear flag: `returned = false`

**Validation Rules**:
- `Provider`, `Symbol`, `EventType` must be set
- `IngestTS` must be > 0
- `Data` must be non-nil and valid JSON

---

### 4. MergedEvent (Multi-Provider Merged Event)

**Purpose**: Multi-provider merged event from Orchestrator windowing

**Lifecycle**: Get() when window closes → Dispatch to consumers → Put() after fan-out

**Fields**:
```go
type MergedEvent struct {
    // Ownership tracking
    returned bool
    
    // Merge metadata
    MergeID    string   // Unique merge window identifier
    Symbol     string   // Canonical symbol
    EventType  int      // Event type (must be same across all fragments)
    WindowOpen int64    // Window open timestamp (nanoseconds)
    WindowClose int64   // Window close timestamp (nanoseconds)
    
    // Fragments
    Fragments  []CanonicalEvent  // Merged fragments (one per provider)
    
    // Status
    IsComplete bool     // True if all expected providers present
    
    // Observability
    TraceID    string
}
```

**Reset() Behavior**:
- Zero all fields
- Release slice: `Fragments = nil` (slice of structs, not pointers)
- Clear flag: `returned = false`

**Validation Rules**:
- `MergeID` must be non-empty
- `Fragments` length must be > 0
- All fragments must have same `Symbol` and `EventType`

---

### 5. OrderRequest (Outbound Order Request)

**Purpose**: Outbound order request envelope

**Lifecycle**: Get() when order created → Send to provider → Put() after response/timeout

**Fields**:
```go
type OrderRequest struct {
    // Ownership tracking
    returned bool
    
    // Request identity
    ClientOrderID string  // Client-assigned order ID (idempotency key)
    Provider      string  // Target provider
    Symbol        string  // Canonical symbol
    
    // Order parameters
    Side          string  // "buy" or "sell"
    OrderType     string  // "limit", "market", etc.
    Quantity      string  // Decimal string (avoid float)
    Price         string  // Decimal string (limit orders only)
    
    // Lifecycle
    CreatedAt     int64   // Request creation timestamp (nanoseconds)
    TimeoutAt     int64   // Request timeout (nanoseconds)
    
    // Observability
    TraceID       string
    DecisionID    string
}
```

**Reset() Behavior**:
- Zero all fields
- Clear flag: `returned = false`

**Validation Rules**:
- `ClientOrderID`, `Provider`, `Symbol` must be non-empty
- `Side` must be "buy" or "sell"
- `Quantity` and `Price` must be valid decimal strings
- `CreatedAt` must be > 0

---

### 6. ExecReport (Execution Report Event)

**Purpose**: Execution report event for order lifecycle tracking

**Lifecycle**: Get() when exec report received → Route to consumers → Put() after fan-out

**Fields**:
```go
type ExecReport struct {
    // Ownership tracking
    returned bool
    
    // Report identity
    ClientOrderID  string  // Matches OrderRequest.ClientOrderID
    ExchangeOrderID string // Provider-assigned order ID
    Provider       string  // Provider name
    Symbol         string  // Canonical symbol
    
    // Execution details
    Status         string  // "new", "filled", "canceled", etc.
    FilledQty      string  // Decimal string
    RemainingQty   string  // Decimal string
    AvgPrice       string  // Decimal string
    
    // Lifecycle
    TransactTime   int64   // Exchange transaction time (nanoseconds)
    ReceivedAt     int64   // Receipt timestamp (nanoseconds)
    
    // Observability
    TraceID        string
    DecisionID     string
}
```

**Reset() Behavior**:
- Zero all fields
- Clear flag: `returned = false`

**Validation Rules**:
- `ClientOrderID`, `Provider`, `Symbol`, `Status` must be non-empty
- `ReceivedAt` must be > 0

---

## Pool Management Entities

### 7. BoundedPool (Pool Wrapper with Capacity)

**Purpose**: Wraps sync.Pool with capacity discipline and timeout

**Fields**:
```go
type BoundedPool struct {
    pool     *sync.Pool        // Underlying sync.Pool
    sem      chan struct{}     // Semaphore for capacity (buffered channel)
    newFunc  func() interface{} // Constructor for new objects
    poolName string             // Pool identifier (for logging)
}
```

**Operations**:
- **Get(ctx context.Context) (interface{}, error)**: Acquire object with timeout (100ms)
- **Put(obj interface{})**: Return object to pool after Reset()

**State Transitions**:
```
[Idle] --Get()--> [Acquired] --Put()--> [Idle]
       <--timeout-- [Waiting]
```

---

### 8. PoolManager (Centralized Pool Coordinator)

**Purpose**: Manages lifecycle of all bounded pools; tracks in-flight objects for shutdown

**Fields**:
```go
type PoolManager struct {
    pools      map[string]*BoundedPool  // Pool registry (keyed by struct type name)
    inFlight   *sync.WaitGroup          // Tracks Get() - Put() balance
    shutdownCh chan struct{}            // Shutdown signal
    mu         sync.RWMutex             // Protects pools map
}
```

**Operations**:
- **RegisterPool(name string, capacity int, newFunc func() interface{})**: Register new pool
- **Get(poolName string, ctx context.Context) (interface{}, error)**: Get from named pool
- **Put(poolName string, obj interface{})**: Put to named pool
- **Shutdown(ctx context.Context) error**: Graceful shutdown with timeout (5s)

**Lifecycle State Machine**:
```
[Running] --Shutdown()--> [Draining] --AllReturned/Timeout--> [Stopped]
```

---

### 9. DispatcherClone (Heap-Allocated Event Clone)

**Purpose**: Heap-allocated per-subscriber clone (not pooled)

**Lifecycle**: Created during Dispatcher fan-out → Enqueued to subscriber → GC after handler

**Fields**: Same as source pooled struct (e.g., CanonicalEvent, MergedEvent, ExecReport)

**Ownership Rule**: Consumer owns clone; no Put() called; GC reclaims after handler returns

**Clone Strategy**:
- **Shallow Copy**: Immutable fields (strings, ints, timestamps)
- **Deep Copy**: Mutable fields (slices, maps) via `append([]T(nil), src...)`

---

## Entity Relationships

```
WsFrame (pooled)
   └─→ Parse ─→ ProviderRaw (pooled)
                     └─→ Normalize ─→ CanonicalEvent (pooled)
                                           ├─→ Route ─→ Dispatcher
                                           │            ├─→ Clone ─→ Consumer 1 (heap, GC)
                                           │            ├─→ Clone ─→ Consumer 2 (heap, GC)
                                           │            └─→ Clone ─→ Consumer 3 (heap, GC)
                                           └─→ Put() to pool
                     
CanonicalEvent[] (pooled fragments)
   └─→ Merge ─→ MergedEvent (pooled)
                     └─→ Route ─→ Dispatcher
                                  ├─→ Clone ─→ Consumer 1 (heap, GC)
                                  └─→ Put() to pool

OrderRequest (pooled)
   └─→ Send ─→ Provider ─→ ExecReport (pooled)
                                └─→ Route ─→ Dispatcher
                                             ├─→ Clone ─→ Consumer 1 (heap, GC)
                                             └─→ Put() to pool
```

---

## Pool Lifecycle State Diagram

```
┌─────────┐
│  Pool   │
│  Init   │
└────┬────┘
     │
     v
┌─────────────────┐      Get() timeout      ┌──────────┐
│  Available      │────────────────────────→│  Error   │
│  (has objects)  │                          └──────────┘
└────┬────────────┘
     │ Get() success
     v
┌─────────────────┐
│  Object         │
│  In-Flight      │
└────┬────────────┘
     │ Put()
     v
┌─────────────────┐      Reset()            ┌──────────────┐
│  Return to Pool │─────────────────────────→│  Available   │
└─────────────────┘                          └──────────────┘
     │ double-Put()
     v
┌─────────────────┐
│  Panic          │
└─────────────────┘
```

---

## Validation Rules Summary

| Entity | Key Constraints |
|--------|----------------|
| WsFrame | Provider non-empty; Data non-nil; ReceivedAt > 0 |
| ProviderRaw | Provider non-empty; Payload valid JSON |
| CanonicalEvent | Provider, Symbol, EventType set; IngestTS > 0; Data non-nil |
| MergedEvent | MergeID non-empty; Fragments length > 0; consistent Symbol/EventType |
| OrderRequest | ClientOrderID, Provider, Symbol non-empty; CreatedAt > 0; valid decimals |
| ExecReport | ClientOrderID, Provider, Symbol, Status non-empty; ReceivedAt > 0 |
| BoundedPool | Capacity > 0; newFunc non-nil; timeout 100ms |
| PoolManager | Shutdown timeout 5s; track in-flight via WaitGroup |

---

## Migration Notes

- **Existing Structs**: Add `returned bool` field (unexported) to each pooled type
- **Constructor Pattern**: Replace `&CanonicalEvent{...}` with `pool.Get("CanonicalEvent", ctx)` 
- **Deallocation Pattern**: Replace scope-exit GC with explicit `pool.Put("CanonicalEvent", event)`
- **Clone Pattern**: Use `event.Clone()` method (generates heap copy) in Dispatcher fan-out
- **Testing**: Add Reset() unit tests per struct type; verify all fields zeroed

---

## Performance Characteristics

| Pool Type | Est. Size (bytes) | Expected Get/Put Rate | Pool Capacity |
|-----------|-------------------|----------------------|---------------|
| WsFrame | ~1KB (avg frame) | 1000/sec | 200 objects |
| ProviderRaw | ~2KB (avg payload) | 1000/sec | 200 objects |
| CanonicalEvent | ~1KB (normalized) | 1000/sec | 300 objects |
| MergedEvent | ~5KB (3 fragments) | 100/sec | 50 objects |
| OrderRequest | ~500B | 10/sec | 20 objects |
| ExecReport | ~500B | 10/sec | 20 objects |

**Total Pool Memory**: ~1.5MB at capacity (vs ~60MB/min allocation without pooling)

