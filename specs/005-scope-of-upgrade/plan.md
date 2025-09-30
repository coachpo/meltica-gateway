# Implementation Plan: Event Distribution & Lifecycle Optimization

**Branch**: `005-scope-of-upgrade` | **Date**: 2025-10-14 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/005-scope-of-upgrade/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/commands/plan.md` for the execution workflow.

## Summary

This feature upgrades the event distribution pipeline to achieve parallel fan-out delivery, centralized memory lifecycle management via Recycler, and structured concurrency. The primary requirements are:

1. **Parallel Fan-out**: Replace heap-allocated clones with pool-backed duplicates delivered simultaneously to all subscribers (15ms for 10 subscribers vs 100ms+ sequential)
2. **Recycler Pattern**: Enforce single return gateway for all event structures across Orchestrator partials, Dispatcher originals, and Consumer deliveries with debug poisoning and double-put guards
3. **Smart Filtering**: Enable consumers to ignore market-data during routing flips while guaranteeing 100% delivery of critical events (ExecReport, ControlAck, ControlResult)
4. **Structured Concurrency**: Migrate from async/pool to github.com/sourcegraph/conc for worker pools with proper error propagation and goroutine lifecycle management

Technical approach: Modify Dispatcher fan-out logic to use conc.Pool for parallel duplicate creation from sync.Pool; implement Recycler component with debug mode and guards; wrap consumer lambdas with auto-recycle and routing_version filtering; maintain existing libraries (coder/websocket, goccy/go-json, sync.Pool) per constitutional requirements.

## Technical Context

**Language/Version**: Go 1.25  
**Primary Dependencies**: 
- `github.com/sourcegraph/conc` (structured concurrency - NEW)
- `github.com/coder/websocket` (existing WebSocket library)
- `github.com/goccy/go-json` (existing JSON library)
- `sync.Pool` (Go standard library - existing memory pooling)

**Storage**: In-memory only (event routing/buffering; no persistent storage)  
**Testing**: Go standard testing (`go test`), race detector (`-race`), coverage (`≥70%`)  
**Target Platform**: Linux x86_64 server (trading monolith)  
**Project Type**: Single monolithic Go application (existing codebase modification)  
**Performance Goals**: 
- Parallel fan-out: <15ms delivery to 10 subscribers
- Memory pool utilization: <80% under 1000 events/sec load
- Zero memory growth over 24-hour run (leak-free)
- >90% parallelism efficiency for 5+ subscriber fan-outs

**Constraints**: 
- No breaking changes to existing component boundaries (LM-01)
- Must maintain existing event schema and versioning (LM-02)
- Zero backward compatibility shims (CQ-08, GOV-04)
- Must eradicate all async/pool usage (PERF-09)
- CI gates: build, race detector, ≥70% coverage, banned import checks

**Scale/Scope**: 
- 500-2000 events/second under normal load
- 5-20 subscribers per event (up to 50 in extreme cases)
- Existing codebase: ~50K LOC
- Components modified: Dispatcher, Orchestrator, Consumer runtime (new Recycler component)

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

[Gates determined based on constitution file]
- Respect immutable component boundaries (LM-01): Providers (WS+REST+Adapter+Book Assembler), Orchestrator (windowed merge), Dispatcher (ordering+routing), Data Bus, Consumers (with local Trading Switch). Control Bus feeds Orchestrator and Dispatcher.
- Use canonical, versioned event schemas (LM-02); treat merged events as first-class on the Data Bus.
- Enforce strong per-stream ordering in Dispatcher with small `seq_provider` buffer; fallback to `ingest_ts` on gaps; no global ordering (LM-03).
- Apply backpressure: latest-wins coalescing for market data; NEVER drop execution lifecycle events (LM-04).
- Implement windowed merge: open on first, close by time or count; late=drop; partial=suppress (LM-05).
- Ensure idempotent orders via `client_order_id` and a lossless ExecReport path (LM-06).
- Assemble orderbooks provider-side using snapshot+diff with checksums and periodic event-driven refresh (LM-07).
- Observability is ops-only with trace_id/decision_id propagation and DLQ; consumers MUST NOT ingest telemetry (LM-08).
- On restart, discard in-flight merges and do not replay windows (LM-09).
- ALWAYS use goccy/go-json for JSON; FORBID encoding/json (PERF-04).
- Use coder/websocket for WebSocket; FORBID gorilla/websocket (PERF-05).
- Use sync.Pool for canonical events and hot-path structs; race-free, bounded pools. Fan-out duplicates from sync.Pool; deliver in parallel; recycle original via Recycler (PERF-06).
- Recycler is the single return gateway for all structs (Orchestrator partials, Dispatcher originals, Consumer deliveries). Enable debug poisoning; guard against double-put. Dispatcher fan-out: clone per-subscriber as heap objects (unpooled); Put() original to Recycler after enqueue; consumers own clones (PERF-07).
- Consumers are pure lambdas; may ignore market-data based on routing_version. Critical kinds (ExecReport, ControlAck, ControlResult) are ALWAYS delivered (PERF-08).
- Use github.com/sourcegraph/conc for all worker pools; FORBID async/pool (PERF-09).
- Honor `/lib` boundaries (ARCH-01/02) and the no-backward-compatibility policy (CQ-08/GOV-04).
- When using Cursor/agents, append "use context7" for current library docs (GOV-06).

## Project Structure

### Documentation (this feature)

```
specs/[###-feature]/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```
/home/qing/work/meltica/
├── core/
│   ├── dispatcher/           # MODIFY: Fan-out logic with conc.Pool
│   ├── orchestrator/         # MODIFY: Recycler integration for partials
│   ├── recycler/             # NEW: Centralized return gateway
│   └── consumer/             # MODIFY: Lambda wrapper with auto-recycle
├── lib/
│   └── pool/                 # EXISTING: sync.Pool infrastructure
├── internal/
│   └── telemetry/            # EXISTING: Metrics and logging
└── tests/
    ├── dispatcher_test.go    # UPDATE: Parallel fan-out tests
    ├── recycler_test.go      # NEW: Debug poisoning, double-put tests
    └── integration/          # UPDATE: End-to-end lifecycle tests
        └── fanout_test.go
```

**Structure Decision**: Single monolithic Go application with component-based organization. This feature modifies existing `core/dispatcher` and `core/orchestrator` components, adds new `core/recycler` component, and enhances `core/consumer` runtime wrapper. All components respect immutable boundaries (LM-01) with Dispatcher handling routing/ordering, Orchestrator managing windowed merge, and Recycler serving as the single return gateway for memory lifecycle management. Library code remains under `/lib` per ARCH-01/02.

## Complexity Tracking

*Fill ONLY if Constitution Check has violations that must be justified*

**No constitutional violations.** This feature fully complies with all MUST requirements:
- Respects immutable boundaries (LM-01) - no changes to Provider, Data Bus, or Control Bus
- Uses existing sync.Pool infrastructure (PERF-06)
- Implements new Recycler component for single return gateway (PERF-07)
- Adds consumer wrapper for auto-recycle and routing_version filtering (PERF-08)
- Migrates from async/pool to github.com/sourcegraph/conc (PERF-09)
- Zero backward compatibility per CQ-08/GOV-04
