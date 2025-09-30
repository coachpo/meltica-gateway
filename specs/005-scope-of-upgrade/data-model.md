# Data Model: Event Distribution & Lifecycle Optimization

**Feature**: 005-scope-of-upgrade  
**Date**: 2025-10-14  
**Status**: Complete

## Overview

This document defines the entity structure and relationships for event distribution, memory lifecycle management, and consumer runtime components. All entities support memory pooling and recycling via centralized Recycler gateway.

---

## Core Entities

### 1. Event

**Description**: Canonical market data or order lifecycle message allocated from sync.Pool, delivered to consumers, and recycled via Recycler.

**Fields**:

| Field | Type | Validation | Description |
|-------|------|------------|-------------|
| `TraceID` | `string` | Non-empty | Unique trace identifier for observability (propagated from Provider) |
| `RoutingVersion` | `uint64` | >= 0 | Current routing topology version (stamped by Orchestrator) |
| `Kind` | `EventKind` | Valid enum | Event type: MarketData, ExecReport, ControlAck, ControlResult |
| `Payload` | `interface{}` | Non-nil | Type-specific data (OrderBookSnapshot, Trade, ExecutionReport, etc.) |
| `IngestTS` | `time.Time` | Non-zero | Provider ingestion timestamp |
| `SeqProvider` | `uint64` | >= 0 | Provider sequence number for ordering |
| `ProviderID` | `string` | Non-empty | Source provider identifier (e.g., "binance", "coinbase") |

**State Transitions**:
```
[Pool] -Get()-> [InUse] -Deliver-> [ConsumerProcessing] -Recycle()-> [Pool]
                   |                                         ^
                   +------ Orchestrator Merge ---------------+
                   +------ Dispatcher Fan-out ---------------+
```

**Invariants**:
- Event MUST have RoutingVersion stamped before Dispatcher enqueue
- Event MUST be recycled exactly once (enforced by Recycler double-put guard)
- Critical events (ExecReport, ControlAck, ControlResult) MUST NOT be filtered

**Pool Integration**:
- Allocated via `eventPool.Get().(*Event)`
- Recycled via `recycler.RecycleEvent(ev)`
- Reset on recycle: clear all fields except pool metadata

---

### 2. EventKind (Enum)

**Description**: Event classification for filtering and delivery guarantees.

**Values**:

| Value | Critical? | Description |
|-------|-----------|-------------|
| `KindMarketData` | No | Price updates, order book snapshots, trades (may be filtered during flips) |
| `KindExecReport` | Yes | Execution reports for order lifecycle (always delivered) |
| `KindControlAck` | Yes | Control plane acknowledgments (always delivered) |
| `KindControlResult` | Yes | Control command results (always delivered) |

**Methods**:
- `IsCritical() bool`: Returns true for ExecReport, ControlAck, ControlResult

---

### 3. Recycler

**Description**: Centralized resource return gateway that receives events from all pipeline stages, performs reset/poisoning, validates against double-put, and returns structures to memory pools.

**Fields**:

| Field | Type | Description |
|-------|------|-------------|
| `eventPool` | `*sync.Pool` | Pool for canonical Event structs |
| `mergedEventPool` | `*sync.Pool` | Pool for MergedEvent structs |
| `execReportPool` | `*sync.Pool` | Pool for ExecReport structs |
| `debugMode` | `bool` | Enable debug poisoning and double-put tracking |
| `putTracker` | `sync.Map` | Tracks recycled pointers to detect double-put (debug mode only) |
| `metrics` | `*RecyclerMetrics` | Observability counters (recycles, double-puts, etc.) |

**Methods**:

| Method | Parameters | Returns | Description |
|--------|------------|---------|-------------|
| `RecycleEvent` | `ev *Event` | - | Reset event fields, poison if debug mode, return to pool |
| `RecycleMergedEvent` | `mev *MergedEvent` | - | Reset merged event, return to pool |
| `RecycleExecReport` | `er *ExecReport` | - | Reset execution report, return to pool |
| `RecycleMany` | `events []*Event` | - | Bulk recycle (optimized for Orchestrator partial cleanup) |
| `EnableDebugMode` | - | - | Enable poisoning and double-put tracking |
| `DisableDebugMode` | - | - | Disable debug features (production mode) |

**State Transitions**:
```
Component -> Recycler.RecycleX() -> Reset() -> [Debug: Poison + Track] -> Pool.Put()
```

**Invariants**:
- Recycler is singleton (global instance)
- All Put() operations MUST go through Recycler (no direct pool.Put())
- Debug mode: double-put triggers panic with stack trace
- Debug mode: poisoned memory causes panic on access

**Metrics**:
- `recycler_events_total`: Counter of events recycled by type
- `recycler_double_puts_total`: Counter of double-put violations detected
- `recycler_recycle_duration_seconds`: Histogram of recycle operation latency

---

### 4. ConsumerWrapper

**Description**: Infrastructure component that wraps consumer lambda functions to provide automatic resource cleanup on return or panic, routing_version-based filtering for market-data, and guaranteed delivery of critical events.

**Fields**:

| Field | Type | Description |
|-------|------|-------------|
| `consumerID` | `string` | Unique consumer identifier for logging |
| `minAcceptVersion` | `uint64` | Minimum routing version to accept (atomic) |
| `recycler` | `*Recycler` | Reference to global Recycler |
| `metrics` | `*ConsumerMetrics` | Per-consumer latency and error tracking |

**Methods**:

| Method | Parameters | Returns | Description |
|--------|------------|---------|-------------|
| `Invoke` | `ctx context.Context, ev *Event, lambda ConsumerFunc` | `error` | Execute lambda with auto-recycle and version filtering |
| `UpdateMinVersion` | `version uint64` | - | Update minimum acceptable routing version (atomic) |
| `ShouldProcess` | `ev *Event` | `bool` | Check if event should be processed (version + critical kind logic) |

**Behavior**:
```go
func (w *ConsumerWrapper) Invoke(ctx context.Context, ev *Event, lambda ConsumerFunc) (err error) {
    defer func() {
        if p := recover(); p != nil {
            err = fmt.Errorf("consumer panic: %v", p)
            w.metrics.PanicsTotal.Inc()
        }
        w.recycler.RecycleEvent(ev) // Always recycle
    }()

    if !w.ShouldProcess(ev) {
        return nil // Skip filtered events
    }

    start := time.Now()
    err = lambda(ctx, ev)
    w.metrics.ProcessingDuration.Observe(time.Since(start).Seconds())
    return err
}
```

**Invariants**:
- Event MUST be recycled exactly once (via defer)
- Critical events MUST bypass version filtering
- Panic MUST NOT prevent recycle

**Metrics**:
- `consumer_invocations_total{consumer_id}`: Counter of lambda invocations
- `consumer_processing_duration_seconds{consumer_id}`: Histogram of processing time
- `consumer_panics_total{consumer_id}`: Counter of panic recoveries
- `consumer_filtered_events_total{consumer_id}`: Counter of version-filtered events

---

### 5. FanoutDuplicate

**Description**: Per-subscriber event copy created from memory pool during parallel delivery. Independent lifecycle from original event; owned by subscriber until ConsumerWrapper recycles it.

**Fields**: Same as Event (full clone)

**Lifecycle**:
```
Dispatcher.Fanout() -> pool.Get() -> Clone(original) -> Deliver -> ConsumerWrapper -> Recycler
```

**Invariants**:
- Created only when fan-out count > 1
- Original event recycled after all duplicates delivered
- Duplicates are heap-allocated (NOT pooled) per PERF-07
- Each duplicate recycled independently

**Notes**:
- If fan-out == 1: original delivered directly (no duplicate creation)
- Duplicates do NOT go back to same pool as original (heap ownership prevents double-put)

---

## Entity Relationships

```
┌─────────────┐
│   Provider  │
└──────┬──────┘
       │ Get() Event from Pool
       ▼
┌─────────────────┐
│  Orchestrator   │
└──────┬──────────┘
       │ 1. Stamp RoutingVersion
       │ 2. Merge (if needed)
       │ 3. Recycle partials via Recycler
       ▼
┌─────────────────┐
│   Dispatcher    │
└──────┬──────────┘
       │ Fan-out (if subscribers > 1)
       │
       ├──> Duplicate 1 (pool.Get) ──> ConsumerWrapper A ──> Recycler
       ├──> Duplicate 2 (pool.Get) ──> ConsumerWrapper B ──> Recycler
       └──> Original              ──> Recycler (after all duplicates sent)
```

**Key Flows**:
1. **Single Subscriber**: Original → ConsumerWrapper → Recycler
2. **Multiple Subscribers**: Original → N Duplicates (parallel) → Recycler (original + N duplicates)
3. **Merge Path**: Partials → MergedEvent (pool.Get) → Recycler (partials) → Dispatcher

---

## Validation Rules

### Event Validation
- `TraceID`: Must be non-empty UUID format
- `RoutingVersion`: Must be >= 0; stamped by Orchestrator before Dispatcher
- `Kind`: Must be valid EventKind enum value
- `Payload`: Must be non-nil; type matches Kind

### Recycler Validation
- Debug mode: Panic if double-put detected
- Debug mode: Panic if poisoned memory accessed
- All pools: Must be initialized before first RecycleX() call

### ConsumerWrapper Validation
- `minAcceptVersion`: Must be <= current RoutingVersion (updated atomically)
- Critical events: MUST NOT be filtered (bypass version check)
- Defer: MUST execute recycle on all code paths (return, panic)

---

## Concurrency Safety

### Event
- Read-only after stamp (RoutingVersion set once by Orchestrator)
- No concurrent writes after creation (immutable during delivery)

### Recycler
- Thread-safe via sync.Map for putTracker
- Pool operations thread-safe (sync.Pool guarantee)
- Metrics increments use atomic operations

### ConsumerWrapper
- `minAcceptVersion` updated atomically (atomic.StoreUint64)
- Invoke() may run concurrently for different events
- Metrics are concurrent-safe (prometheus counters/histograms)

---

## Debug Mode Behavior

When `Recycler.debugMode == true`:

1. **Poison on Recycle**: First 8 bytes set to `0xDEADBEEFDEADBEEF`
2. **Track Pointers**: `putTracker.LoadOrStore()` detects double-put
3. **Panic on Double-Put**: Immediate failure with stack trace
4. **Panic on Use-After-Put**: Field access triggers segfault or poison check

**Production Mode** (`debugMode == false`):
- Skip poisoning (performance)
- Skip putTracker (memory overhead)
- Rely on normal Go panic if use-after-put occurs

---

## Migration Notes

### From Current State
- **Dispatcher**: Replace heap-allocated clones with pool.Get() duplicates
- **Orchestrator**: Call `recycler.RecycleMany(partials)` after merge
- **Consumers**: No changes (wrapper handles recycle automatically)
- **Providers**: No changes (Get() paths unchanged)

### Breaking Changes
- Direct `eventPool.Put()` calls FORBIDDEN (use Recycler)
- async/pool imports FORBIDDEN (use github.com/sourcegraph/conc)

---

## Next Phase

**Phase 1 Complete**: Data model defined. Proceed to contract generation and quickstart guide.

