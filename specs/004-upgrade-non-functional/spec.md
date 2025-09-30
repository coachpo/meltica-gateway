# Feature Specification: Performance & Memory Architecture Upgrade

**Feature Branch**: `004-upgrade-non-functional`  
**Created**: 2025-10-13  
**Status**: Draft  
**Input**: User description: "Upgrade non-functional requirements and dataflow (struct-based Data Bus; unpooled fan-out clones): Replace WebSocket client with coder/websocket across all providers. Replace JSON marshalling/unmarshalling with goccy/go-json across Providers, Orchestrator, Dispatcher, Consumers. Introduce sync.Pool-backed structs for: WsFrame, ProviderRaw, CanonicalEvent, MergedEvent, OrderRequest, ExecReport envelopes; implement Reset() for zeroing. DataBus boundary: Dispatcher allocates per-subscriber clones on the heap (no pooling), enqueues them, then immediately Put()s the original pooled struct. Consumers must treat received clones as ephemeral, do not Put() or retain beyond handler scope. Enforce linting to ban encoding/json and non-coder websockets; CI fails if violated."

## Clarifications

### Session 2025-10-13

- Q: When a pool reaches its capacity limit and Get() is called, what should happen? → A: Block until object available (with timeout)
- Q: How should double-Put() violations be detected and handled? → A: Runtime panic on double-Put (fail-fast)
- Q: What timeout duration should be used when Get() blocks waiting for a pooled object? → A: 100ms timeout (balanced with latency target)
- Q: How should Reset() method completeness be validated? → A: Unit tests verify all fields zeroed
- Q: How should the system handle in-flight pooled objects during graceful shutdown? → A: Wait for all in-flight Put() with timeout (5s)

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Reduced Memory Footprint Under Load (Priority: P1)

As a system operator, when the trading monolith processes high-volume market data streams, the system maintains stable memory usage and avoids garbage collection pressure, ensuring predictable performance during peak trading hours.

**Why this priority**: Memory efficiency is critical for system stability. Without object pooling, high-frequency message processing causes excessive allocations and GC pauses, leading to increased latency and potential service degradation during critical trading periods.

**Independent Test**: Deploy the upgraded system with pooled structs, run a sustained load test with 50 symbols across 3 providers for 30 minutes, and verify memory usage remains stable with reduced GC pause times compared to the baseline non-pooled implementation.

**Acceptance Scenarios**:

1. **Given** the system is processing 1000 market data events per second, **When** monitoring memory metrics over a 30-minute period, **Then** heap allocations are reduced by at least 40% compared to the non-pooled baseline
2. **Given** the system is under sustained load, **When** measuring GC pause times, **Then** p99 GC pause duration is under 10ms
3. **Given** multiple consumers are subscribed to the same symbols, **When** the Dispatcher performs fan-out, **Then** memory usage grows linearly with subscriber count without pool contention or memory leaks

---

### User Story 2 - Improved Message Processing Latency (Priority: P1)

As a system operator, when market data flows through the pipeline (WS read → Orchestrator → Dispatcher → Data Bus → Consumers), end-to-end latency is minimized through efficient serialization and reduced allocations, enabling faster order execution decisions.

**Why this priority**: Low latency is essential for trading systems. Faster JSON parsing and reduced memory allocations directly translate to quicker trade execution and better market opportunity capture.

**Independent Test**: Run latency benchmarks measuring the complete pipeline (WebSocket frame receipt through consumer delivery) and verify p99 latency is under 150ms with the new libraries versus baseline performance.

**Acceptance Scenarios**:

1. **Given** a market data event is received via WebSocket, **When** it flows through the entire pipeline to consumers, **Then** p99 end-to-end latency is under 150ms
2. **Given** JSON deserialization is performed using goccy/go-json, **When** processing 10,000 events, **Then** parsing time is at least 30% faster than encoding/json baseline
3. **Given** coder/websocket is handling connections, **When** receiving frames under load, **Then** frame processing overhead is reduced by at least 20% compared to gorilla/websocket

---

### User Story 3 - Memory Safety and Leak Prevention (Priority: P1)

As a developer, the system enforces clear ownership semantics for pooled objects, preventing memory leaks, double-frees, and use-after-free bugs through architectural boundaries and lifecycle rules.

**Why this priority**: Memory safety bugs are critical defects that can cause crashes or data corruption. Clear ownership rules enforced at compile-time and runtime prevent entire classes of bugs.

**Independent Test**: Run the system with race detector and memory leak detection tools for 24 hours under varying load, confirming no memory leaks, no double-Put() errors, and no race conditions in pool access.

**Acceptance Scenarios**:

1. **Given** the Dispatcher receives a pooled event, **When** it completes fan-out and calls Put(), **Then** the original struct is returned to the pool exactly once without double-Put errors
2. **Given** a Consumer receives a cloned event, **When** the consumer handler completes, **Then** the clone is eligible for garbage collection without manual Put() calls
3. **Given** pooled structs are accessed concurrently, **When** running with -race flag, **Then** no race conditions are detected in pool operations or struct reuse

---

### User Story 4 - Enforced Library Standards (Priority: P2)

As a developer, the CI system prevents accidental use of banned libraries (encoding/json, gorilla/websocket) through automated checks, ensuring all code adheres to the performance and memory architecture standards.

**Why this priority**: Preventing regressions is easier than fixing them. Automated enforcement ensures the performance benefits are maintained as the codebase evolves.

**Independent Test**: Attempt to introduce code using encoding/json or gorilla/websocket, commit it, and verify the CI build fails with clear error messages identifying the banned imports.

**Acceptance Scenarios**:

1. **Given** a developer adds code importing encoding/json, **When** the CI pipeline runs, **Then** the build fails with an error message stating "encoding/json is forbidden, use goccy/go-json"
2. **Given** a developer adds code importing gorilla/websocket, **When** the CI pipeline runs, **Then** the build fails with an error message stating "gorilla/websocket is forbidden, use coder/websocket"
3. **Given** code using only approved libraries, **When** the CI pipeline runs, **Then** all checks pass without library-related violations

---

### Edge Cases

- What happens when a pooled struct is not properly Reset() before Put()? (Potential data leakage between reuses; prevented by unit tests that verify all fields zeroed)
- How does the system handle pool exhaustion scenarios where Get() cannot acquire a struct? (Blocks with timeout; if timeout expires, returns error to caller for backpressure handling)
- What if a Consumer accidentally retains a reference to a cloned event beyond handler scope? (Potential stale data reads but no memory corruption since clones are unpooled)
- How are concurrent Put() calls to the same pool handled? (sync.Pool handles concurrency; double-Put() on same object triggers panic with stack trace)
- What happens during graceful shutdown with in-flight pooled objects? (Wait up to 5s for all Put() operations; log unreturned objects if timeout expires)

## Requirements *(mandatory)*

**Compatibility Note**: Breaking APIs/import paths are allowed (CQ-08, GOV-04). Do not ship shims or feature flags for old contracts. Features MUST: use canonical, versioned event schemas (LM-02); respect immutable component boundaries (LM-01); enforce per‑stream ordering in Dispatcher with `seq_provider` buffer and `ingest_ts` fallback, no global ordering (LM-03); apply backpressure with latest‑wins for market data while NEVER dropping execution lifecycle events (LM-04); follow windowed merge rules (open on first, close by time or count; late=drop; partial=suppress) (LM-05); ensure idempotent orders via `client_order_id` and lossless ExecReport path (LM-06); assemble orderbooks provider-side with snapshot+diff, checksums, periodic event-driven refresh (LM-07); keep observability ops-only with trace/decision IDs and DLQ (LM-08); ALWAYS use goccy/go-json for JSON and FORBID encoding/json (PERF-04); use coder/websocket and FORBID gorilla/websocket (PERF-05); employ sync.Pool for canonical events and hot-path structs with race-free, bounded pools (PERF-06); and follow Dispatcher fan-out ownership rules (clone per-subscriber, unpooled; Put() original after enqueue) (PERF-07). Maintain `/lib` boundaries (ARCH-01/02).

### Functional Requirements

#### Library Replacement

- **FR-001**: System MUST replace all WebSocket client implementations with coder/websocket across all provider adapters (Binance, future providers)
- **FR-002**: System MUST replace all JSON marshalling and unmarshalling operations with goccy/go-json across Providers, Orchestrator, Dispatcher, and Consumers
- **FR-003**: System MUST remove all imports of encoding/json and gorilla/websocket from the codebase
- **FR-004**: WebSocket connections MUST use coder/websocket's context-aware API for connection lifecycle management

#### Object Pooling Implementation

- **FR-005**: System MUST implement sync.Pool-backed pools for the following struct types: WsFrame, ProviderRaw, CanonicalEvent, MergedEvent, OrderRequest, ExecReport
- **FR-006**: Each pooled struct type MUST implement a Reset() method that zeros all fields to prevent data leakage between reuses; unit tests MUST verify all fields are zeroed after Reset()
- **FR-007**: Pools MUST be race-free and use sync.Pool's built-in concurrency safety mechanisms
- **FR-008**: Pools MUST implement capacity discipline to prevent unbounded growth; when pool is at capacity, Get() blocks with 100ms timeout until an object becomes available, returning error on timeout
- **FR-009**: No long-lived references to pooled memory MUST exist beyond handler scope boundaries

#### Data Bus Ownership Rules

- **FR-010**: Dispatcher MUST allocate per-subscriber clones as regular heap objects (not from pools) during fan-out operations
- **FR-011**: Dispatcher MUST call Put() on the original pooled struct immediately after all subscriber clones are enqueued
- **FR-012**: Consumers MUST NOT call Put() on received cloned events; clones are owned by consumers until garbage collected
- **FR-013**: System MUST ensure the original pooled struct is Put() exactly once per processing cycle; double-Put() attempts MUST trigger runtime panic with clear error message and stack trace
- **FR-014**: Cloned events delivered to Consumers MUST be ephemeral and not retained beyond the handler scope

#### Pipeline Integration

- **FR-015**: WebSocket frame reading MUST use pooled WsFrame structs, Get() from pool on receive, Put() after parsing
- **FR-016**: Provider adapters MUST use pooled ProviderRaw structs for exchange-specific payloads before normalization
- **FR-017**: Canonical event normalization MUST produce pooled CanonicalEvent structs
- **FR-018**: Orchestrator windowed merge MUST produce pooled MergedEvent structs
- **FR-019**: Order request and execution report paths MUST use pooled OrderRequest and ExecReport structs
- **FR-020**: All Put() operations MUST occur after the pooled struct is no longer needed and before any potential reallocation

#### CI Enforcement

- **FR-021**: CI MUST include static analysis checks to detect imports of encoding/json
- **FR-022**: CI MUST include static analysis checks to detect imports of gorilla/websocket and other non-approved WebSocket libraries
- **FR-023**: CI build MUST fail if any banned imports are detected, with clear error messages identifying the violation
- **FR-024**: Linting rules MUST be configured to forbid banned imports at the import statement level
- **FR-025**: CI MUST validate that all tests pass with -race flag to detect concurrency issues in pool operations

#### Graceful Shutdown

- **FR-026**: During graceful shutdown, system MUST wait for all in-flight pooled objects to be returned via Put() with a 5-second timeout
- **FR-027**: If shutdown timeout expires with outstanding pooled objects, system MUST log the count and identifiers of unreturned objects before completing shutdown

### Key Entities

- **WsFrame**: Raw WebSocket frame data received from provider connections, pooled for reuse
- **ProviderRaw**: Exchange-specific raw event payload before normalization, pooled for reuse
- **CanonicalEvent**: Normalized event in Meltica canonical schema (BookUpdate, Trade, Ticker, etc.), pooled for reuse
- **MergedEvent**: Multi-provider merged event from Orchestrator windowing, pooled for reuse
- **OrderRequest**: Outbound order request envelope, pooled for reuse
- **ExecReport**: Execution report event for order lifecycle tracking, pooled for reuse
- **PoolManager**: Manages lifecycle of sync.Pool instances for each struct type (conceptual entity for pool coordination)
- **DispatcherClone**: Heap-allocated per-subscriber clone of a pooled event, owned by Consumer until GC

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: System achieves at least 40% reduction in heap allocations per second when processing 1000 events/second compared to non-pooled baseline
- **SC-002**: End-to-end message latency (WebSocket receive to Consumer delivery) p99 is under 150ms under sustained load
- **SC-003**: JSON parsing performance improves by at least 30% measured in events processed per second compared to encoding/json baseline
- **SC-004**: System runs for 24 hours under load with zero memory leaks detected by memory profiling tools
- **SC-005**: GC pause times remain under 10ms at p99 during sustained high-throughput operation
- **SC-006**: All tests pass with -race flag enabled, confirming no race conditions in pool operations
- **SC-007**: CI successfully blocks commits containing banned imports (encoding/json, gorilla/websocket) with 100% detection rate
- **SC-008**: WebSocket connection overhead is reduced by at least 20% compared to gorilla/websocket baseline
- **SC-009**: Pool Get() operations that timeout after 100ms return error to caller, enabling proper backpressure handling

## Assumptions

- Existing test coverage is sufficient to validate behavior equivalence after library replacements
- coder/websocket and goccy/go-json APIs are compatible enough with current usage patterns to allow straightforward migration
- Performance baseline metrics exist or can be captured before the upgrade for comparison
- sync.Pool default behavior (unbounded size, LIFO access) is acceptable; if strict capacity limits are needed, wrapper logic will be implemented
- Pool capacity discipline can be enforced through architectural review and runtime monitoring rather than hard limits (unless SC-004 reveals leaks requiring stricter controls)
- All struct types identified for pooling (WsFrame, ProviderRaw, etc.) have deterministic lifecycles suitable for pooling
- Consumers currently do not retain references beyond handler scope, so the "no Put() by consumers" rule won't require defensive checks

## Dependencies

- go.mod must add: github.com/goccy/go-json and github.com/coder/websocket
- go.mod must remove: github.com/gorilla/websocket (or mark as unused if transitive)
- CI configuration must support custom linting rules or import restrictions (e.g., golangci-lint with depguard or custom script)
- Performance benchmarking infrastructure must be available to validate success criteria (SC-001, SC-002, SC-003, SC-008)
- Memory profiling tools must be integrated to validate SC-004 and SC-005

## Out of Scope

- Changing the Data Bus implementation from struct-based to interface-based or other architectural patterns
- Implementing alternative pooling strategies (e.g., ring buffers, sync.Map-based pools)
- Adding metrics or observability specifically for pool statistics (covered by general memory/performance metrics)
- Migrating other non-core structs to pooling (only the six specified types are in scope)
- Performance tuning beyond library replacement and pooling (e.g., algorithmic optimizations, concurrency model changes)
- Backward compatibility shims or gradual migration paths (per CQ-08/GOV-04, this is a breaking upgrade)
