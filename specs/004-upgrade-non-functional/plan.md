# Implementation Plan: Performance & Memory Architecture Upgrade

**Branch**: `004-upgrade-non-functional` | **Date**: 2025-10-13 | **Spec**: [spec.md](./spec.md)  
**Input**: Feature specification from `/specs/004-upgrade-non-functional/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/commands/plan.md` for the execution workflow.

## Summary

Replace gorilla/websocket with coder/websocket and encoding/json with goccy/go-json across all components (Providers, Orchestrator, Dispatcher, Consumers). Implement sync.Pool-backed object pooling for six hot-path struct types (WsFrame, ProviderRaw, CanonicalEvent, MergedEvent, OrderRequest, ExecReport) with Reset() methods. Enforce struct-based Data Bus with clear ownership: Dispatcher creates per-subscriber heap clones during fan-out, Put()s original to pool; Consumers own clones until GC. Add CI guards to ban encoding/json and gorilla/websocket imports. Target: 40% allocation reduction, <150ms p99 latency, 30% faster JSON parsing, zero memory leaks over 24hrs.

## Technical Context

**Language/Version**: Go 1.25  
**Primary Dependencies**:
  - `github.com/coder/websocket` (WebSocket client, replacing gorilla/websocket)
  - `github.com/goccy/go-json` (JSON encoding/decoding, replacing encoding/json)
  - `sync.Pool` (standard library object pooling)
  - `golang.org/x/time/rate` (token bucket rate limiting, existing)
  - `testify` (testing assertions, existing)

**Storage**: No persistence (configuration state only; no event storage or replay)  
**Testing**: Go test framework with `-race`, ≥70% coverage enforced, per-test and global timeouts, mocks/fakes for WS/REST  
**Target Platform**: Single-process monolith (Linux/macOS), multi-core concurrency  
**Project Type**: Single monolithic application (`cmd/gateway` entry point)  
**Performance Goals**:
  - End-to-end latency <150ms p99 for market data events
  - 40% reduction in heap allocations at 1000 events/sec vs non-pooled baseline
  - 30% faster JSON parsing vs encoding/json
  - 20% reduced WebSocket overhead vs gorilla/websocket
  - GC pause <10ms p99

**Constraints**:
  - Pool Get() timeout: 100ms (blocks with timeout, returns error on expiry)
  - Double-Put() detection: runtime panic with stack trace (fail-fast)
  - Graceful shutdown: 5-second timeout for in-flight Put() operations
  - Strong per-stream ordering (provider, symbol, eventType) maintained
  - NEVER drop execution reports under backpressure
  - Latest-wins coalescing for market data (Ticker/Book/KlineSummary) when backpressure occurs

**Scale/Scope**:
  - 3 providers (Binance active, Coinbase/Kraken planned) with WS+REST clients
  - 50 symbols per provider (150 total concurrent streams)
  - 3+ concurrent consumers with overlapping subscriptions
  - In-process architecture (channels, no external message broker)
  - 6 pooled struct types covering WS read → Orchestrator → Dispatcher → Data Bus pipeline

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

✅ **LM-01: Immutable Component Boundaries**: Architecture preserves existing boundaries: Providers (WS+REST+Adapter+Book Assembler) → Orchestrator (windowed merge) → Dispatcher (ordering+routing) → Data Bus → Consumers. Pooling and library replacements are localized to each component without cross-boundary changes.

✅ **LM-02: Canonical Events & Versioned Schemas**: Feature maintains canonical event schemas (BookUpdate, Trade, Ticker, ExecReport, etc.). Pooled structs use same versioned schema definitions; only lifecycle (Get/Put) changes.

✅ **LM-03: Strong Per-Stream Ordering**: Dispatcher ordering logic unchanged; still uses `seq_provider` buffer with `ingest_ts` fallback. No global ordering attempted.

✅ **LM-04: Backpressure & Drop Policy**: Feature enhances backpressure with pool exhaustion timeout (100ms) but preserves latest-wins for market data and lossless ExecReport handling.

✅ **LM-05: Windowed Merge**: Orchestrator windowing unchanged; 10-second timeout OR 1000-event count. Late=drop, partial=suppress logic preserved.

✅ **LM-06: Idempotent Orders**: Order keying by `client_order_id` and lossless ExecReport path maintained. Pooling applies to request/response envelopes, not deduplication logic.

✅ **LM-07: Snapshot+Diff with Integrity**: Provider-side orderbook assembly unchanged; pooling applies to parsed events post-assembly.

✅ **LM-08: Ops-Only Telemetry**: Observability unchanged; trace_id/decision_id propagation and DLQ maintained. No consumer telemetry ingestion.

✅ **LM-09: Restart Simplicity**: On restart, discard in-flight merges; no replay. Shutdown enhancement (5s timeout for pool drainage) aligns with simplicity principle.

✅ **PERF-04: JSON Library Standard**: Feature directly implements this requirement by replacing all encoding/json usage with goccy/go-json.

✅ **PERF-05: WebSocket Library Standard**: Feature directly implements this requirement by replacing gorilla/websocket with coder/websocket.

✅ **PERF-06: Object Pooling Strategy**: Feature directly implements sync.Pool for canonical events and hot-path structs (WsFrame, ProviderRaw, CanonicalEvent, MergedEvent, OrderRequest, ExecReport) with race-free, bounded pools (100ms timeout).

✅ **PERF-07: Struct Bus Ownership Rules**: Feature directly implements Dispatcher fan-out with per-subscriber heap clones (unpooled); Put() original after enqueue; consumers own clones until GC.

✅ **CQ-08/GOV-04: No Backward Compatibility**: Breaking change accepted. No shims or feature flags. Direct library replacement and pool lifecycle enforcement.

✅ **ARCH-01/02: /lib Boundaries**: Pool wrapper utilities (if needed) will go in `/lib`; business logic in `/internal`. No reverse dependencies.

✅ **TS-01: Coverage Threshold**: CI enforces ≥70% line coverage. Feature includes unit tests for Reset() methods, pool lifecycle, double-Put() detection.

✅ **TS-02: Timeouts Required**: All tests include per-test timeouts (context.WithTimeout); global timeout configured.

✅ **TS-03: Race Detector**: All tests run with `-race`. CI runs `go test ./... -race -count=1`.

✅ **TS-05: IO Isolation**: Mocks/fakes for WebSocket and REST; no real network calls in unit tests.

**Gate Status**: ✅ PASSED - All constitutional requirements satisfied. No violations to justify.

## Project Structure

### Documentation (this feature)

```
specs/004-upgrade-non-functional/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   └── pool_lifecycle.yaml  # Pool API contract (Get/Put/Reset)
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```
internal/
├── adapters/
│   └── binance/
│       ├── ws_client.go       # Replace gorilla → coder websocket
│       ├── parser.go          # Replace encoding/json → goccy/go-json
│       └── provider.go        # Pool WsFrame, ProviderRaw
├── conductor/
│   └── orchestrator_v2.go     # Pool MergedEvent output
├── dispatcher/
│   ├── ingest.go              # Pool CanonicalEvent input
│   ├── stream_ordering.go     # Fan-out with heap clones
│   └── runtime.go             # Put() original after enqueue
├── schema/
│   ├── event.go               # Add Reset() to pooled types
│   └── order.go               # Pool OrderRequest, ExecReport
└── pool/                      # NEW: Pool management utilities
    ├── manager.go             # Centralized pool coordinator
    ├── bounded.go             # Wrapper for capacity + timeout
    └── lifecycle.go           # Double-Put() detection

lib/
└── pool/                      # NEW: Generic pool utilities (if needed)
    └── wrapper.go             # Reusable pool patterns

tests/
├── unit/
│   ├── pool_lifecycle_test.go      # Reset() verification, double-Put() tests
│   ├── websocket_migration_test.go # coder/websocket behavior equivalence
│   └── json_migration_test.go      # goccy/go-json behavior equivalence
└── integration/
    ├── pooling_e2e_test.go         # End-to-end pooling validation
    └── memory_leak_test.go         # 24hr leak detection (gated under build tag)
```

**Structure Decision**: Single monolithic Go application using existing `internal/` structure. New `internal/pool/` package for pool coordination; `lib/pool/` for reusable utilities. Library replacements touch all existing components in-place (WS clients, JSON ops). No new projects or modules created.

## Complexity Tracking

*No constitutional violations. This section intentionally left empty.*
