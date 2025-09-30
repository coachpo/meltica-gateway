# Implementation Plan: [FEATURE]

**Branch**: `[###-feature-name]` | **Date**: [DATE] | **Spec**: [link]
**Input**: Feature specification from `/specs/[###-feature-name]/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/commands/plan.md` for the execution workflow.

## Summary

[Extract from feature spec: primary requirement + technical approach from research]

## Technical Context

<!--
  ACTION REQUIRED: Replace the content in this section with the technical details
  for the project. The structure here is presented in advisory capacity to guide
  the iteration process.
-->

**Language/Version**: [e.g., Python 3.11, Swift 5.9, Rust 1.75 or NEEDS CLARIFICATION]  
**Primary Dependencies**: [e.g., FastAPI, UIKit, LLVM or NEEDS CLARIFICATION]  
**Storage**: [if applicable, e.g., PostgreSQL, CoreData, files or N/A]  
**Testing**: [e.g., pytest, XCTest, cargo test or NEEDS CLARIFICATION]  
**Target Platform**: [e.g., Linux server, iOS 15+, WASM or NEEDS CLARIFICATION]
**Project Type**: [single/web/mobile - determines source structure]  
**Performance Goals**: [domain-specific, e.g., 1000 req/s, 10k lines/sec, 60 fps or NEEDS CLARIFICATION]  
**Constraints**: [domain-specific, e.g., <200ms p95, <100MB memory, offline-capable or NEEDS CLARIFICATION]  
**Scale/Scope**: [domain-specific, e.g., 10k users, 1M LOC, 50 screens or NEEDS CLARIFICATION]

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
<!--
  ACTION REQUIRED: Replace the placeholder tree below with the concrete layout
  for this feature. Delete unused options and expand the chosen structure with
  real paths (e.g., apps/admin, packages/something). The delivered plan must
  not include Option labels.
-->

```
# [REMOVE IF UNUSED] Option 1: Single project (DEFAULT)
src/
├── models/
├── services/
├── cli/
└── lib/

tests/
├── contract/
├── integration/
└── unit/

# [REMOVE IF UNUSED] Option 2: Web application (when "frontend" + "backend" detected)
backend/
├── src/
│   ├── models/
│   ├── services/
│   └── api/
└── tests/

frontend/
├── src/
│   ├── components/
│   ├── pages/
│   └── services/
└── tests/

# [REMOVE IF UNUSED] Option 3: Mobile + API (when "iOS/Android" detected)
api/
└── [same as backend above]

ios/ or android/
└── [platform-specific structure: feature modules, UI flows, platform tests]
```

**Structure Decision**: [Document the selected structure and reference the real
directories captured above]

## Complexity Tracking

*Fill ONLY if Constitution Check has violations that must be justified*

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| [e.g., 4th project] | [current need] | [why 3 projects insufficient] |
| [e.g., Repository pattern] | [specific problem] | [why direct DB access insufficient] |
