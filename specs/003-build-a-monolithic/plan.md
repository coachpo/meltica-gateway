# Implementation Plan: Monolithic Auto-Trading Application

**Branch**: `003-build-a-monolithic` | **Date**: October 12, 2025 | **Spec**: [spec.md](./spec.md)  
**Input**: Feature specification from `/specs/003-build-a-monolithic/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/commands/plan.md` for the execution workflow.

## Summary

Build a single-process, loss-tolerant, non-HFT trading monolith in Go with immutable component boundaries: Providers (WS+REST+Adapter+Book Assembler) emit canonical events to an Orchestrator (windowed merge engine with late-drop/partial-suppress) which feeds a Dispatcher (per-stream ordering, fair-share fan-out, backpressure with coalescing for market data, lossless ExecReport). Consumers subscribe via Control Bus and receive events via in-process Data Bus. Orders are idempotent by client_order_id. No event replay on restart. Observability is ops-only with trace_id/decision_id, metrics, and optional DLQ.

## Technical Context

**Language/Version**: Go 1.25 (idiomatic concurrency, channels, goroutines)  
**Primary Dependencies**:
  - `gorilla/websocket` for WebSocket client connections
  - `golang.org/x/time/rate` for token-bucket rate limiting
  - Standard library (`net/http`, `context`, `sync`, `time`) for core operations
  - Testing: `testify` for assertions, mocks/fakes for WS/REST providers
  
**Storage**: Configuration state only (subscriptions, merge configs) - no event persistence or replay  
**Testing**: Go test framework with `-race`, ≥70% line coverage enforced in CI, per-test and global timeouts, mocks/fakes for external IO  
**Target Platform**: Single-process monolith (Linux/macOS) with multi-core concurrency  
**Project Type**: Single monolithic application (cmd/gateway entry point)  
**Performance Goals**:
  - End-to-end latency <200ms (p99) for market data events
  - Support 3+ concurrent consumers each subscribed to 50 symbols across 3 providers
  - Per-stream ordering buffer: lateness ≈150ms, flush ≈50ms, max 200 events
  - Windowed merge: 10-second window OR 1000-event count threshold
  
**Constraints**:
  - Strong per-stream ordering (provider, symbol, eventType) with seq_provider + ingest_ts fallback
  - NEVER drop execution reports (ExecReport) under backpressure
  - Latest-wins coalescing for Ticker/Book/KlineSummary when backpressure occurs
  - Checksum verification success rate >99.5% for order books
  - Subscription routing updates within 1 second
  - Book resync (gap/checksum/staleness) completes within 5 seconds
  
**Scale/Scope**:
  - 3 providers (Binance, Coinbase, Kraken) with WS+REST clients
  - 10-50 symbols per provider
  - Multiple consumers (3+ concurrent) with overlapping subscriptions
  - In-process architecture (channels/queues, no external message broker)

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

✅ **LM-01: Immutable Component Boundaries**: Architecture strictly follows Providers → Orchestrator → Dispatcher → Data Bus → Consumers with Control Bus feeding Orchestrator/Dispatcher. No cross-component logic leakage.

✅ **LM-02: Canonical Events & Versioned Schemas**: All events normalized to canonical types (BookUpdate, BookSnapshot, Trade, Ticker, ExecReport) with version fields. Merged events are first-class with merge_id.

✅ **LM-03: Strong Per-Stream Ordering**: Dispatcher enforces ordering per (provider, symbol, eventType) using seq_provider with small buffer and ingest_ts fallback. No global ordering attempted.

✅ **LM-04: Backpressure & Drop Policy**: Implements token-bucket rate limiting with latest-wins coalescing for coalescable market data. ExecReport events are NEVER dropped or coalesced under any condition.

✅ **LM-05: Windowed Merge**: Orchestrator opens windows on first event, closes by 10-second timeout OR 1000-event count. Late fragments dropped, partial windows (missing providers) suppressed with telemetry.

✅ **LM-06: Idempotent Orders**: Orders keyed by client_order_id with deduplication. ExecReport path is lossless through entire pipeline.

✅ **LM-07: Snapshot+Diff with Integrity**: Each provider module assembles order books using REST snapshot + WebSocket diffs with sequence verification, checksum validation, and periodic refresh (2-5 min).

✅ **LM-08: Ops-Only Telemetry**: Telemetry/metrics emitted to ops-only queue with trace_id/decision_id propagation. Consumers do not ingest telemetry; DLQ optional for dropped events.

✅ **LM-09: Restart Simplicity**: On restart, discard in-flight merge windows and do not replay. Warm restarts not required for non-HFT use case.

✅ **CQ-08/GOV-04: No Backward Compatibility**: Breaking changes allowed. No shims, no feature flags for old contracts. Remove deprecated paths directly.

✅ **ARCH-01/02: /lib Boundaries**: Business-agnostic reusable code in `/lib` (async pool, telemetry utilities). Domain logic in `/internal` with no reverse dependencies.

✅ **TS-01: Coverage Threshold**: CI enforces ≥70% line coverage at repo level; build fails below threshold.

✅ **TS-02: Timeouts Required**: All tests include per-test timeouts (context.WithTimeout); global timeout configured in test runner.

✅ **TS-03: Race Detector**: All tests run with `-race` flag; CI runs `go test ./... -race -count=1`.

✅ **TS-05: IO Isolation**: Mocks/fakes for WebSocket and REST clients; no real network calls in unit tests.

**Gate Status**: ✅ PASSED - All constitutional requirements satisfied. No violations to justify.

## Project Structure

### Documentation (this feature)

```
specs/003-build-a-monolithic/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── canonical_events.yaml    # Canonical event schemas (BookUpdate, Trade, etc.)
│   ├── control_bus.yaml         # Control messages (subscribe, unsubscribe, set_trading_mode)
│   └── telemetry.yaml           # Telemetry event schemas (book.resync, merge.suppressed_partial)
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```
cmd/
└── gateway/
    └── main.go                     # Entry point: initialize components, start monolith

internal/
├── adapters/
│   ├── binance/                    # Provider O
│   │   ├── ws_client.go           # WebSocket connection management
│   │   ├── rest_client.go         # REST API client for snapshots
│   │   ├── parser.go              # Parse exchange-specific formats
│   │   ├── book_assembler.go      # Snapshot+diff with checksum verification
│   │   └── provider.go            # Emit canonical events to orchestrator
│   ├── coinbase/                   # Provider P (similar structure)
│   └── kraken/                     # Provider Q (similar structure)
│
├── conductor/
│   ├── orchestrator.go             # Windowed merge engine (open/close by time|count)
│   ├── merge_window.go             # Window state management
│   └── forwarder.go                # Forward canonical/merged events to dispatcher
│
├── dispatcher/
│   ├── dispatcher.go               # Main dispatch logic
│   ├── stream_ordering.go          # Per-stream ordering (seq_provider buffer + ingest_ts fallback)
│   ├── routing_table.go            # Map subscriptions to consumers
│   ├── backpressure.go             # Token-bucket rate limiting, fair-share fan-out
│   ├── coalescer.go                # Latest-wins coalescing for Ticker/Book/KlineSummary
│   └── order_handler.go            # Route orders to providers (idempotent by client_order_id)
│
├── bus/
│   ├── databus/                    # In-process Data Bus
│   │   ├── bus.go                 # Event publication/subscription (channels)
│   │   └── memory.go              # In-memory channel-based implementation
│   └── controlbus/                 # In-process Control Bus
│       ├── bus.go                 # Control command publication/subscription
│       └── memory.go              # In-memory channel-based implementation
│
├── consumer/
│   ├── consumer.go                 # Consumer interface and base implementation
│   ├── trading_switch.go           # Local trading switch (enable/disable)
│   └── subscription_manager.go     # Manage active subscriptions per consumer
│
├── schema/
│   ├── event.go                    # Canonical event types (BookUpdate, BookSnapshot, Trade, Ticker, ExecReport)
│   ├── order.go                    # OrderRequest and order lifecycle types
│   └── control.go                  # Control message types (subscribe, unsubscribe, set_trading_mode)
│
├── observability/
│   ├── telemetry.go                # Trace_id/decision_id propagation
│   ├── metrics.go                  # Metrics emission (buffer depth, coalesced drops, throttled ms)
│   ├── logger.go                   # Structured logging
│   └── dlq.go                      # Dead-letter queue for undeliverable events (optional)
│
└── config/
    ├── config.go                   # Configuration loading (providers, symbols, merge rules)
    └── streaming.yaml              # Config file (extends existing streaming.example.yaml)

lib/
├── async/
│   └── pool.go                     # Worker pool for concurrent operations (already exists)
└── telemetry/
    └── otel.go                     # OpenTelemetry utilities (already exists)

tests/
├── unit/
│   ├── book_assembler_test.go      # Test gap detection, checksum verification
│   ├── merge_window_test.go        # Test complete/partial/late window scenarios
│   ├── stream_ordering_test.go     # Test out-of-order input reordering
│   ├── idempotent_orders_test.go   # Test duplicate client_order_id handling
│   └── coalescing_test.go          # Test latest-wins coalescing vs lossless ExecReport
│
└── integration/
    ├── end_to_end_test.go          # Full pipeline test: provider → orchestrator → dispatcher → consumer
    ├── provider_fakes.go           # Mock WebSocket/REST providers (extends binance_fakes.go)
    └── backpressure_test.go        # Test fair-share, token-bucket, coalescing under load
```

**Structure Decision**: Single-process monolith using existing project layout. Extends current `internal/adapters/binance`, `internal/conductor`, `internal/dispatcher`, `internal/bus` with new modules for orchestrator merge logic, per-stream ordering, and consumer management. Reuses existing `lib/async` and `lib/telemetry`. Entry point is `cmd/gateway/main.go`.

## Complexity Tracking

*Fill ONLY if Constitution Check has violations that must be justified*

No violations. All constitutional requirements are satisfied without requiring additional complexity or exceptions.

## Phase 0: Research & Decisions

See [research.md](./research.md) for detailed research outcomes and technology decisions.

**Key Research Areas**:
1. Go concurrency patterns for windowed merge (channels vs mutexes)
2. Per-stream ordering algorithms (priority queue vs sorted buffer)
3. Token-bucket rate limiting implementations (golang.org/x/time/rate)
4. WebSocket client libraries (gorilla/websocket vs nhooyr.io/websocket)
5. Checksum algorithms for order book verification (CRC32 vs provider-specific)
6. Latest-wins coalescing strategies (ring buffer vs map-based)
7. Testing strategies for race-free concurrent code
8. Configuration format for merge rules and provider settings

## Phase 1: Design Artifacts

- **[data-model.md](./data-model.md)**: Canonical event schemas, merge window state, stream ordering state, subscription state
- **[contracts/](./contracts/)**: OpenAPI-style YAML contracts for canonical events, control messages, telemetry
- **[quickstart.md](./quickstart.md)**: Developer onboarding guide with configuration examples and test scenarios

## Phase 2: Implementation Tasks

Generated by `/speckit.tasks` command (not part of this plan output).

## Success Metrics (from spec)

- **SC-001**: End-to-end latency <200ms (p99) during normal market conditions
- **SC-002**: Zero out-of-order delivery for events within 150ms lateness window
- **SC-003**: 100% delivery of execution reports without loss or coalescing
- **SC-004**: Fair-share bandwidth allocation (no consumer <25% of subscribed bandwidth with 3+ consumers)
- **SC-005**: Subscription routing updates within 1 second
- **SC-006**: Order book resync (gap/checksum/staleness) within 5 seconds
- **SC-007**: 100% accuracy preventing duplicate orders (client_order_id reuse)
- **SC-008**: Zero false positives for trading switch enforcement
- **SC-009**: 95% adherence to merge window thresholds (10s or 1000 events)
- **SC-010**: Zero false emissions of partial merge windows
- **SC-011**: Support 3 consumers × 50 symbols × 3 providers without degradation
- **SC-012**: Checksum verification success rate >99.5%

## Completion Summary

✅ **Phase 0 (Research)**: Complete - All technology decisions resolved in [research.md](./research.md)
✅ **Phase 1 (Design)**: Complete - Generated [data-model.md](./data-model.md), [contracts/](./contracts/), [quickstart.md](./quickstart.md)
✅ **Agent Context Update**: Complete - Updated CLAUDE.md with Go 1.25 and project context
✅ **Constitution Re-Check**: PASSED - All constitutional requirements satisfied post-design

### Generated Artifacts

1. **[research.md](./research.md)** - 8 technology decisions resolved:
   - Go concurrency patterns for windowed merge (channels + time.Ticker)
   - Per-stream ordering algorithm (container/heap priority queue)
   - Token-bucket rate limiting (golang.org/x/time/rate)
   - WebSocket client library (gorilla/websocket, already in use)
   - Checksum algorithms (provider-specific: CRC32, SHA256)
   - Latest-wins coalescing strategy (map-based)
   - Testing strategies (race detector, context-based cancellation, fake time)
   - Configuration format (YAML extension of existing config)

2. **[data-model.md](./data-model.md)** - Complete canonical data model:
   - 6 canonical event types (BookSnapshot, BookUpdate, Trade, Ticker, ExecReport, base Event)
   - Merge window state machine (Open → Closed/Suppressed)
   - Stream ordering state (StreamBuffer with EventHeap)
   - Subscription and routing table structures
   - Order lifecycle (OrderRequest → ExecReport states)
   - Consumer and Trading Switch models
   - Control message schemas
   - Telemetry event structures
   - Metrics definitions

3. **[contracts/canonical_events.yaml](./contracts/canonical_events.yaml)** - OpenAPI 3.0 schemas:
   - BaseEvent with all required fields (event_id, routing_version, seq_provider, etc.)
   - EventType enum (BookSnapshot, BookUpdate, Trade, Ticker, ExecReport, KlineSummary)
   - Payload schemas for all event types
   - PriceLevel structure (decimal strings, no floats)
   - ExecReportState enum (ACK, PARTIAL, FILLED, CANCELLED, REJECTED, EXPIRED)
   - Example events (BookSnapshot, Trade, ExecReport, MergedBookSnapshot)

4. **[contracts/control_bus.yaml](./contracts/control_bus.yaml)** - Control message schemas:
   - ControlMessage base structure
   - ControlMessageType enum (Subscribe, Unsubscribe, MergedSubscribe, SetTradingMode)
   - Payload schemas for all control message types
   - MergeConfig structure (window_duration, max_events, partial_policy)
   - ControlAcknowledgement with success/error responses
   - Example control messages and acknowledgements

5. **[contracts/telemetry.yaml](./contracts/telemetry.yaml)** - Ops-only telemetry schemas:
   - TelemetryEvent base structure with trace_id/decision_id
   - TelemetryEventType enum (8 event types)
   - Severity levels (INFO, WARN, ERROR)
   - Metadata schemas for each telemetry event type
   - Example telemetry events (book.resync, merge.suppressed_partial, etc.)

6. **[quickstart.md](./quickstart.md)** - Developer onboarding guide:
   - Prerequisites and setup instructions
   - Configuration examples (providers, orchestrator, dispatcher, consumers)
   - Running the monolith (startup commands, health checks)
   - Interaction examples (subscribe, merged-subscribe, order submission)
   - Monitoring and telemetry (Prometheus metrics, distributed tracing)
   - Testing instructions (unit tests, integration tests, fake providers)
   - Common scenarios and troubleshooting

7. **[CLAUDE.md](../../CLAUDE.md)** - Updated agent context:
   - Added Go 1.25 (idiomatic concurrency, channels, goroutines)
   - Added configuration state database (subscriptions, merge configs)
   - Added project type (single monolithic application)

### Re-Evaluated Constitution Check

All constitutional requirements remain satisfied post-design:

✅ **LM-01**: Immutable boundaries preserved in data model (Providers → Orchestrator → Dispatcher → Consumers)
✅ **LM-02**: Canonical events with versioned schemas defined in contracts/canonical_events.yaml
✅ **LM-03**: Per-stream ordering implemented via StreamBuffer with seq_provider priority queue
✅ **LM-04**: Backpressure with coalescing for coalescable types; ExecReport never dropped
✅ **LM-05**: Windowed merge state machine defined (open on first, close by time/count, late=drop, partial=suppress)
✅ **LM-06**: Idempotent orders via client_order_id; lossless ExecReport path
✅ **LM-07**: Provider-side book assembly with BookAssembler interface (snapshot+diff, checksums, periodic refresh)
✅ **LM-08**: Ops-only telemetry with trace_id/decision_id propagation
✅ **LM-09**: Restart simplicity (discard in-flight merges, no replay)
✅ **TS-01/02/03/05**: Testing standards satisfied (≥70% coverage, timeouts, -race, IO isolation)
✅ **ARCH-01/02**: /lib boundaries respected (async pool, telemetry utilities)
✅ **CQ-08/GOV-04**: No backward compatibility coding

## Next Steps

**Ready for Phase 2**: Run `/speckit.tasks` to generate implementation tasks with milestones and acceptance criteria.
