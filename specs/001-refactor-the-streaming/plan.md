# Implementation Plan: Refactor Streaming Routing Flow to Dispatcher-Conductor Architecture

**Branch**: `001-refactor-the-streaming` | **Date**: 2025-10-12 | **Spec**: `/specs/001-refactor-the-streaming/spec.md`
**Input**: Feature specification from `/specs/001-refactor-the-streaming/spec.md`

## Summary

Refactor the Meltica streaming pipeline so Binance ingestion produces canonical Meltica Events, the Dispatcher governs routing and control-plane updates, and the Conductor orchestrates fused outputs before publishing to the Data Bus—while excising the legacy Router, Coordinator, and Filter Adapter components—in tandem with new internal packages, configuration, and integration scaffolding.

## Technical Context

**Language/Version**: Go 1.25 (repository standard; satisfies Go ≥1.22 requirement)  
**Primary Dependencies**: Go standard library, `github.com/gorilla/websocket`, `github.com/goccy/go-json`, internal `errs` package, OpenTelemetry SDK with OTLP/HTTP exporter configured via YAML/env, pluggable bus interfaces with in-memory stub  
**Storage**: In-memory SnapshotStore with compare-and-swap semantics plus pluggable adapters (e.g., Redis) conforming to the same interface  
**Testing**: `go test ./... -race`, integration smoke suites in `tests/integration` using fake WS/REST endpoints, snapshot store, and bus implementations  
**Target Platform**: Long-running backend service deployments (Linux/macOS containers)  
**Project Type**: Backend streaming pipeline refactor  
**Performance Goals**: ≤1s delivery for 99% orchestrated events, ≤2s control-plane propagation, sustained throughput under Binance WS + REST load with bounded channels  
**Constraints**: Canonical-first ingestion, idempotent events, OpenTelemetry traces/metrics, bounded worker pools for backpressure, no backward compatibility shims  
**Scale/Scope**: Single-exchange (Binance) pipeline with multi-consumer Data Bus fan-out and per-instrument snapshot collaboration

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

All constitution gates presently satisfied by the proposed architecture:
- ✅ EP-01: Binance Adapter produces canonical Meltica Events before any Dispatcher or Conductor logic executes.
- ✅ EP-02 & EP-03: Single Binance Adapter owns parsing; Dispatcher maintains the authoritative dispatch table and handles canonicalization.
- ✅ EP-04 & EP-05: Conductor remains downstream of Dispatcher while the Control Bus drives subscribe/unsubscribe flows within the 2s SLA.
- ✅ EP-06 & EP-07: Canonical events will carry stable keys, sequencing, and trace context; SnapshotStore updates remain atomic per instrument with end-to-end latency metrics.
- ✅ EP-08, ARCH-01/02, CQ-08, GOV-06: Refactor removes legacy Router/Coordinator, respects `/lib` boundaries, and disregards backward compatibility.

## Project Structure

### Documentation (this feature)

```
specs/001-refactor-the-streaming/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
└── contracts/
```

### Source Code (repository root)

```
cmd/
└── gateway/               # Entry point wiring adapters, dispatcher, conductor, buses

config/                    # YAML/env defaults for topics, REST schedules, snapshot TTL

internal/
├── adapters/
│   └── binance/           # WS/REST clients, payload parsers, canonical raw instances
├── bus/
│   ├── controlbus/        # Subscribe/Unsubscribe command abstractions
│   └── databus/           # Canonical Meltica Event pub/sub interfaces + stubs
├── conductor/             # Meltica Conductor orchestration, throttling, analytics hooks
├── dispatcher/            # Dispatch table, canonicalization, control-plane handlers
├── schema/                # Canonical types, RawInstance aliases, control messages
└── snapshot/              # SnapshotStore interface with in-memory + pluggable implementations

lib/                       # Shared observability/config utilities reused across components

tests/
└── integration/           # E2E smoke tests using fake WS/REST, snapshot, and bus components
```

**Structure Decision**: Adopt internal subpackages to mirror Adapter → Dispatcher → Conductor flow while keeping shared utilities in `lib` and wiring in `cmd/gateway`.

## Complexity Tracking

No constitution violations requiring justification at this stage.
