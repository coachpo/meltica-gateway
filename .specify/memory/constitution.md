<!--
SYNC IMPACT REPORT
==================

Version Change: v5.0.0 → v5.1.0

Modified Principles:
- PERF-06: Enhanced with fan-out duplicate strategy using sync.Pool for per-subscriber copies and parallel delivery
- PERF-07: Enhanced with Recycler as single return gateway, debug poisoning, and double-put guards
- GOV-04: Updated to explicitly forbid async/pool and require conc library migration

Added Sections:
- PERF-08: Consumer Purity Rules (pure lambdas, routing_version-based market-data ignoring, critical kinds always delivered)
- PERF-09: Concurrency Library Standard (MUST use github.com/sourcegraph/conc, FORBID async/pool)
- GOV-06: Developer Workflow Guidance (use context7 prompt for Cursor/agents)

Removed Sections:
- None

Templates Requiring Updates:
✅ .specify/templates/plan-template.md (Constitution Check updated with PERF-08, PERF-09, GOV-06)
✅ .specify/templates/spec-template.md (Compatibility Note updated with concurrency requirements)
✅ .specify/templates/tasks-template.md (Notes section updated with Recycler, conc migration, consumer purity tasks)

Deferred/Follow-ups:
- None

Rationale:
MINOR bump (5.0.0 → 5.1.0): Refinements and additions to performance/concurrency architecture.
Enhanced PERF-06/PERF-07 with Recycler pattern, debug poisoning, and parallel fan-out strategy.
New PERF-08 establishes consumer purity rules with routing_version-based filtering and mandatory
critical event delivery. New PERF-09 mandates github.com/sourcegraph/conc library for all worker
pools and forbids async/pool. Added GOV-06 for developer workflow (use context7 in Cursor).
These are additive/refinement changes that don't break existing principle structure.

Last Amended: 2025-10-14
-->

# Meltica Trading Monolith Constitution

## 1. Architecture & Boundaries (Immutable)

**LM-01: Immutable Component Boundaries (MUST)**
Providers (WS+REST+Adapter+Book Assembler), Orchestrator (windowed merge), Dispatcher (ordering+routing), Data Bus, and Consumers (with local Trading Switch) are fixed components. Control Bus feeds Orchestrator and Dispatcher. Telemetry is ops-only and never consumed by business logic.
Rationale: Stable boundaries enable simple reasoning, localize failures, and avoid scope creep.

**LM-02: Canonical Events & Versioned Schemas (MUST)**
All payloads are normalized to canonical events with versioned schemas. Merged events are first-class citizens on the Data Bus.
Rationale: Versioned contracts prevent schema drift and enable safe evolution without shims.

## 2. Loss Tolerance & Ordering Policy

**LM-03: Strong Per-Stream Ordering (MUST)**
Guarantee ordering per provider stream. Dispatcher applies best-effort ordering using `seq_provider` with a small buffer; fallback to `ingest_ts` if gaps cannot be resolved. No global ordering is attempted.
Rationale: Non-HFT tolerance allows local ordering guarantees without global coordination.

**LM-04: Backpressure & Drop Policy (MUST)**
Latest-wins coalescing for market data; NEVER drop execution lifecycle events.
Rationale: Market data can be lossy; order lifecycle must be lossless for correctness.

## 3. Windowed Merge Semantics

**LM-05: Windowed Merge (MUST)**
Open a window on first event; close by time or count. Late events are dropped; partial windows are suppressed (not emitted).
Rationale: Deterministic windowing simplifies downstream logic and avoids partial states.

## 4. Orders & Execution Path

**LM-06: Idempotent Orders (MUST)**
Orders are keyed by `client_order_id`. The ExecReport path is lossless and idempotent.
Rationale: Idempotence prevents duplicate actions and ensures consistent reconciliation.

## 5. Provider-Side Orderbook Assembly

**LM-07: Snapshot+Diff with Integrity (MUST)**
Assemble orderbooks at the provider adapter using snapshot+diff with checksums and periodic, event-driven refresh.
Rationale: Local assembly reduces latency and isolates exchange-quirk handling at the edge.

## 6. Observability & Telemetry

**LM-08: Ops-Only Telemetry (MUST)**
Emit metrics and propagate `trace_id`/`decision_id`. Maintain DLQ/telemetry exclusively for ops; consumers MUST NOT ingest telemetry.
Rationale: Keeps business data clean while ensuring operability and debuggability.

## 7. Restart & Simplicity

**LM-09: Restart Simplicity (MUST)**
On restart, discard in-flight merges; do not replay windows.
Rationale: Favors operational simplicity over completeness in a non-HFT setting.

## 8. Code Quality Principles

### 8.1 Core Design Philosophy

**CQ-01: SDK-First Architecture (MUST)**  
Core packages define canonical contracts; adapters implement those contracts. Keep behavior uniform and prevent adapter type leakage in public APIs.

**CQ-02: Layer Purity (MUST)**  
Respect component boundaries in LM-01; avoid circular deps; keep `/lib` generic.

**CQ-03: Zero Floating-Point for Money (MUST)**  
Use `*big.Rat` for monetary values; forbid floats in public APIs. Preserve decimal precision in JSON.

**CQ-04: Canonical Symbol Format (MUST)**  
Use `BASE-QUOTE` uppercase (e.g., `BTC-USDT`) in public APIs. Translate at adapter boundaries.

**CQ-05: Exhaustive Enum Mapping (MUST)**  
Use explicit switch mappings; no silent defaults. Unknown values return errors.

**CQ-06: Typed Errors (MUST)**  
Return `*errs.E` with canonical codes. Include exchange name, HTTP status, and provider error details.

**CQ-07: Documentation as Code (SHOULD)**  
Exported types and functions have concise godoc describing expected behavior.

**CQ-08: Backward Compatibility (MUST-NOT)**  
NO backward-compatibility coding. Remove/deprecate legacy paths instead of shimming. No feature flags to support old contracts.

**CQ-09: Observability Hooks (SHOULD)**  
Provide structured logging hooks and optional metrics callbacks without forcing a stack.

**CQ-10: Static Code Inspection (MUST)**  
Run `golangci-lint` on all code changes. Resolve reported issues before committing. Use `make lint` locally to catch problems early.

*Rationale*: Static analysis catches bugs, enforces consistent style, identifies dead code, and improves maintainability. golangci-lint aggregates multiple linters efficiently and provides actionable feedback without manual code review overhead.

## 9. Testing Standards

**TS-01: Coverage Threshold (MUST)**  
Enforce ≥70% line coverage at repo level.

**TS-02: Timeouts Required (MUST)**  
All tests include per-test and global timeouts to prevent hangs.

**TS-03: Race Detector Always (MUST)**  
Run tests with `-race`. CI runs `go test ./... -race -count=1`.

**TS-04: Determinism (SHOULD)**  
Avoid sleeps; use contexts, fakes, and clock injection.

**TS-05: IO Isolation (MUST)**  
Use mocks/fakes for external IO (WS, REST, file/network). No real network in unit tests.

**TS-06: Integration Tests (SHOULD)**  
Gate under `//go:build integration`; skipped by default; run locally when needed.

## 10. User Experience Consistency

**UX-01: Uniform Error Messages (SHOULD)**  
Use a consistent structure: "[exchange] op failed: reason (code: X)" for predictable parsing.

**UX-02: Context Propagation (MUST)**  
All I/O honors `context.Context` cancellation and deadlines.

**UX-03: Graceful Degradation (SHOULD)**  
Single-symbol/exchange failures must not crash the monolith; reconnect/backoff.

**UX-04: Idiomatic Go APIs (MUST)**  
Follow Go idioms: explicit error returns, channels for async events, and `Close()` for cleanup.

## 11. Performance Requirements

**PERF-01: Bounded Buffers (SHOULD)**  
Prefer bounded allocations and buffer reuse in hot paths.

**PERF-02: Rate Limit Compliance (MUST)**  
Respect provider limits; use token buckets/backoff when needed.

**PERF-03: Connection Reuse (SHOULD)**  
Reuse HTTP connections and keep WebSockets persistent.

**PERF-04: JSON Library Standard (MUST)**  
ALWAYS use `github.com/goccy/go-json` for all JSON encode/decode operations. FORBID `encoding/json` in all code paths.

*Rationale*: goccy/go-json provides significantly better performance with lower allocations. Consistent library usage prevents inconsistent behavior and enables predictable optimization.

**PERF-05: WebSocket Library Standard (MUST)**  
Use `github.com/coder/websocket` for all WebSocket connections. FORBID `github.com/gorilla/websocket` and other WebSocket libraries.

*Rationale*: coder/websocket offers modern API design, better context support, and lower overhead. Single library reduces maintenance burden and ensures consistent behavior.

**PERF-06: Object Pooling Strategy (MUST)**  
Use `sync.Pool` for canonical Meltica Event structs and all hot-path message structs from WS read → Orchestrator → Dispatcher → Data Bus. Pools must be race-free and bounded via capacity discipline. No long-lived references to pooled memory beyond handler scope. Fan-out duplicates: create per-subscriber duplicates from sync.Pool; deliver in parallel; recycle the original via Recycler after enqueue loop.

*Rationale*: Reduces GC pressure and allocation overhead in high-throughput scenarios. Bounded pools prevent unbounded memory growth. Parallel delivery maximizes throughput while sync.Pool-backed duplicates maintain memory efficiency.

**PERF-07: Struct Bus Ownership & Recycler Rules (MUST)**  
Recycler is the single return gateway for all structs (from Orchestrator partials, Dispatcher originals, and Consumer deliveries). Enable debug poisoning to catch use-after-put; guard against double-put. Dispatcher creates per-subscriber duplicates from sync.Pool for parallel delivery. After all duplicates are enqueued, Dispatcher Put()s the original back to its pool via Recycler. Consumers receive pooled duplicates and recycle them via ConsumerWrapper after processing (return or panic).

*Rationale*: Centralized Recycler pattern prevents double-free bugs and use-after-free errors. Debug poisoning catches lifecycle violations early. Pool-backed duplicates reduce allocations while ConsumerWrapper auto-recycle ensures leak-free operation. Clear ownership semantics (Recycler as sole return gateway) prevent pool corruption.

**PERF-08: Consumer Purity Rules (MUST)**  
Consumers are pure lambdas; they may ignore market-data during flips (based on routing_version). Critical kinds (ExecReport, ControlAck, ControlResult) are ALWAYS delivered and must NOT be ignored.

*Rationale*: Pure lambdas ensure stateless, predictable consumer behavior. Routing-version-based filtering prevents stale data processing during topology changes while guaranteeing delivery of mission-critical events (order lifecycle, control plane acknowledgments) regardless of routing state.

**PERF-09: Concurrency Library Standard (MUST)**  
Use `github.com/sourcegraph/conc` (e.g., `conc.Group`, `conc/pool`) for all worker pools, including Dispatcher fan-out workers and other goroutine pools. FORBID any `async/pool` usage. Remove/eradicate all prior `async/pool` integrations; no shims, no dual paths.

*Rationale*: conc provides structured concurrency with better error handling, context propagation, and panic recovery compared to ad-hoc goroutine pools. Single concurrency library ensures consistent patterns and reduces maintenance burden. No backward compatibility per CQ-08/GOV-04.

## 12. Governance Framework

### 12.1 How Principles Guide Technical Decisions

**GOV-01: Decision Authority (MUST)**  
Single maintainer decides. No RFCs, councils, or formal reviews. Prefer direct commits.

**GOV-02: Automated Gates (MUST)**  
Build, `-race` tests, ≥70% coverage, and `golangci-lint` all pass.

**GOV-02b: Automated Enforcement Inventory**  
Current CI (`.github/workflows/ci.yml`):  
- Setup Go `1.25`  
- `make build`  
- `go test ./... -race -count=1`
- Static checks to forbid banned imports: `encoding/json`, `gorilla/websocket`, `async/pool`
- Coverage reporting with ≥70% threshold

Local quality gates (run before commit):  
- `make lint` — runs `golangci-lint run` to catch style, bugs, and complexity issues

**GOV-03: Implementation Choices (SHOULD)**  
Choose the simplest path that satisfies MUST principles. Default to idiomatic Go and existing patterns.

**GOV-04: Backward Compatibility (MUST-NOT)**  
No shims, no feature flags for old contracts, remove legacy paths. Eradicate prior `async/pool` integrations; migrate to `conc` with no dual code paths.

**GOV-05: Evolution (MUST)**  
Update this constitution via direct commit; keep it practical.

**GOV-06: Developer Workflow Guidance (SHOULD)**  
When using Cursor (or supported agents), append "use context7" to prompts so Context7 MCP injects current, version-specific docs/examples and avoids outdated APIs.

*Rationale*: Context7 integration ensures developers receive up-to-date library documentation (e.g., conc, goccy/go-json, coder/websocket) matching current versions, reducing integration errors and API misuse.

## 13. Architecture Boundaries

**ARCH-01: Core Library Residency (MUST)**  
Reusable, business‑agnostic infrastructure lives under `/lib`.

**ARCH-02: Domain Package Purity (MUST)**  
Domain packages contain business logic only; reusable helpers belong in `/lib`.

**ARCH-03: Dependency Direction (MUST)**  
Domain code may depend on `/lib`; `/lib` must not depend on domain packages.

**ARCH-04: Import Paths May Change (MUST)**  
No stability guarantees for package names/imports. Rename/move freely; no shims.

### Appendix: Severity Levels

- **MUST**: Blocking requirement; do not knowingly violate  
- **SHOULD**: Strong recommendation; justify if you skip  
- **INFO**: Advisory; best practice

**Version**: 5.1.0 | **Ratified**: 2025-10-12 | **Last Amended**: 2025-10-14

---

## Version History

### v5.1.0 (2025-10-14)
MINOR: Enhanced PERF-06 with fan-out duplicate strategy using sync.Pool for per-subscriber copies and parallel delivery. Enhanced PERF-07 with Recycler as single return gateway, debug poisoning to catch use-after-put, and double-put guards. Added PERF-08 (Consumer Purity Rules): pure lambdas, routing_version-based market-data ignoring, critical kinds (ExecReport, ControlAck, ControlResult) always delivered. Added PERF-09 (Concurrency Library Standard): MUST use github.com/sourcegraph/conc, FORBID async/pool. Updated GOV-02b to include async/pool in banned imports. Updated GOV-04 to explicitly mandate async/pool eradication. Added GOV-06 (Developer Workflow Guidance): use context7 prompt for Cursor/agents to get current library docs. No backward compatibility per CQ-08/GOV-04.

### v5.0.0 (2025-10-13)
MAJOR: Added mandatory runtime performance & memory requirements. Introduced PERF-04 (MUST use goccy/go-json, FORBID encoding/json), PERF-05 (MUST use coder/websocket, FORBID gorilla/websocket), PERF-06 (object pooling with sync.Pool for canonical events and hot-path structs), PERF-07 (struct bus ownership rules: Dispatcher fan-out clones, pool lifecycle). Updated GOV-02b to include CI guards for banned imports and coverage enforcement. Removed duplicate principles (old PERF-06, UX-06). No backward compatibility layers per CQ-08/GOV-04.

### v4.0.0 (2025-10-12)
Redefined principles for a loss-tolerant, non-HFT monolith: immutable boundaries; canonical, versioned events; per-stream ordering; explicit backpressure; windowed merge rules; idempotent orders; provider-side book assembly; ops-only telemetry; simple restarts; and stricter testing gates (coverage/timeouts).

### v3.0.0 (2025-10-12)
Introduced Meltica Event Pipeline principles (EP-01 through EP-08) to mandate canonical-first ingestion, Dispatcher authority, control plane isolation, end-to-end observability, and refactor directives (Router → Dispatcher, Coordinator removal, Filter Adapter replacement).

### v2.1.0 (2025-10-12)
Added CQ-10: Static Code Inspection principle requiring `golangci-lint` for all code changes. Updated GOV-02b (Automated Enforcement Inventory) and GOV-04 (Quality Gates) to reflect the new linting requirement. Makefile already provides `make lint` target; developers must resolve linter issues before committing.

### v2.0.0 (2025-10-12)
Lean solo‑dev rewrite. Removed bureaucratic governance (RFCs, formal reviews), simplified testing to essentials, aligned with current CI (build + `go test -race`), and codified "MUST ALWAYS IGNORE BACKWARD COMPATIBILITY".
