# Tasks: Refactor Streaming Routing Flow to Dispatcher-Conductor Architecture

**Input**: Design documents from `/specs/001-refactor-the-streaming/`
**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: Integration smoke testing is required per FR-011; dedicated tasks are included under the relevant user stories.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`
- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which phase or user story this task belongs to (Setup, Foundation, US1, US2, US3, Polish)
- Include exact file paths in descriptions

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Establish shared configuration scaffolding, remove legacy components, and prepare the service entrypoint

- [X] T001 [Setup] Remove legacy Router module and references (e.g., delete `internal/router/`, legacy router command wiring under `cmd/`).
- [X] T002 [Setup] Remove Coordinator component packages/config (e.g., `internal/coordinator/`, related bootstrap code, configuration hooks).
- [X] T003 [Setup] Replace Filter Adapter usages with Binance adapter references across `config/`, `internal/dispatcher/`, and `cmd/` packages.
- [X] T004 [Setup] Add sample streaming configuration at `config/streaming.example.yaml` covering adapter topics, REST pollers, snapshot TTL, bus buffer sizes, and telemetry endpoint defaults.
- [X] T005 [Setup] Scaffold `cmd/gateway/main.go` to load config, initialize logging, and block on context cancellation without yet wiring pipeline components.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

- [X] T006 [Foundation] Create `internal/schema/event.go` defining `CanonicalType`, `MelticaEvent`, `RawInstance`, control bus messages, validation helpers, and idempotency key utilities.
- [X] T007 [P] [Foundation] Implement YAML streaming config loader in `config/streaming.go` including dispatcher routes, REST schedules, snapshot TTL, bus buffers, and telemetry options.
- [X] T008 [P] [Foundation] Add OpenTelemetry initializer with OTLP/HTTP fallback in `lib/telemetry/otel.go` exposing trace/metric providers for gateway use.
- [X] T009 [P] [Foundation] Introduce bounded worker-pool helpers in `lib/async/pool.go` to enforce dispatcher backpressure settings.
- [X] T010 [P] [Foundation] Define `databus.Bus` interface and in-memory implementation with bounded channels and per-instrument fan-out in `internal/bus/databus/bus.go` and `internal/bus/databus/memory.go`.
- [X] T011 [P] [Foundation] Define `controlbus.Bus` interface and in-memory implementation supporting command consumption queues in `internal/bus/controlbus/bus.go` and `internal/bus/controlbus/memory.go`.
- [X] T012 [P] [Foundation] Implement `snapshot.Store` interface plus in-memory CAS store with version counters in `internal/snapshot/store.go` and `internal/snapshot/memory_store.go`.
- [X] T013 [Foundation] Establish dispatcher routing primitives (`Route`, `RestFn`, `FilterRule`, dispatch table management) in `internal/dispatcher/table.go` with filter evaluation utilities.

**Checkpoint**: Foundation ready — gateway can rely on schema, buses, snapshot store, and dispatcher table definitions.

---

## Phase 3: User Story 1 – Canonical Event Delivery for Consumers (Priority: P1) 🎯 MVP

**Goal**: Deliver canonical Meltica Events from Binance WS/REST sources through the Dispatcher and Conductor to the Data Bus.

**Independent Test**: Trigger fake Binance WS frames and REST snapshots; assert subscribers receive canonical `MelticaEvent` with expected metadata via Data Bus.

### Tests for User Story 1 (required by FR-011)

- [X] T014 [US1] Add Binance WS/REST fakes for integration testing in `tests/integration/binance_fakes.go` supporting ticker and order book payloads.
- [X] T015 [US1] Write `tests/integration/canonical_delivery_test.go` verifying canonical events flow from adapter → dispatcher → conductor → databus for WS and REST scenarios.

### Implementation for User Story 1

- [X] T016 [P] [US1] Implement streaming WS client in `internal/adapters/binance/ws_client.go` consuming frames into `schema.RawInstance` with trace propagation hooks.
- [X] T017 [P] [US1] Implement REST snapshot poller in `internal/adapters/binance/rest_client.go` fetching JSON snapshots on configured intervals and emitting `RawInstance` slices.
- [X] T018 [P] [US1] Add Binance payload parsing and normalization in `internal/adapters/binance/parser.go` mapping exchange payloads to canonical keys and numeric types.
- [X] T019 [US1] Build dispatcher ingestion pipeline in `internal/dispatcher/ingest.go` applying filter rules, canonicalizing to `schema.MelticaEvent`, sequencing per instrument, and emitting drop/convert metrics.
- [X] T020 [US1] Implement conductor pass-through stage in `internal/conductor/forwarder.go` forwarding canonical events downstream while recording latency span attributes.
- [X] T021 [US1] Extend `internal/bus/databus/memory.go` to publish canonical events to subscribers with latency measurement and bounded buffers.
- [X] T022 [US1] Wire `cmd/gateway/main.go` to initialize telemetry, start Binance adapter goroutines, connect dispatcher ingestion, and publish to the databus implementation.

**Checkpoint**: User Story 1 delivers canonical events end-to-end and passes integration smoke tests.

---

## Phase 4: User Story 2 – Dynamic Subscription Control (Priority: P2)

**Goal**: Allow operations teams to manage subscriptions via Control Bus commands, updating dispatcher routes and native Binance subscriptions within 2 seconds.

**Independent Test**: Issue subscribe/unsubscribe HTTP control requests and confirm dispatch table updates, native topic adjustments, and control acknowledgements within SLA.

### Tests for User Story 2

- [X] T023 [US2] Write `tests/integration/control_dispatch_test.go` validating control commands trigger dispatch table updates, audit events, and native subscription changes inside the SLA window.

### Implementation for User Story 2

- [X] T024 [US2] Implement dispatcher control handlers in `internal/dispatcher/control.go` to process `Subscribe`/`Unsubscribe`, mutate dispatch table, emit audit events, and schedule native updates.
- [X] T025 [P] [US2] Implement Binance subscription manager in `internal/adapters/binance/subscriptions.go` coordinating WS topic (subscribe/unsubscribe) and REST poller lifecycle actions from dispatcher signals.
- [X] T026 [US2] Extend `internal/bus/controlbus/memory.go` to maintain dispatch versions, respond with acknowledgements, and log malformed command errors.
- [X] T027 [P] [US2] Build HTTP control API server complying with `contracts/controlbus.yaml` in `internal/dispatcher/control_http.go`, bridging requests to the control bus.
- [X] T028 [US2] Update `cmd/gateway/main.go` to serve the control HTTP API, connect control bus consumers, and ensure SLA metrics are exported.

**Checkpoint**: User Stories 1 & 2 operate independently—canonical delivery with dynamic subscription control.

---

## Phase 5: User Story 3 – Orchestrated Event Fusion (Priority: P3)

**Goal**: Fuse WS deltas and REST snapshots with throttling and snapshot collaboration, publishing enriched Meltica Events.

**Independent Test**: Simulate mixed WS/REST input; confirm conductor refreshes snapshot store atomically, throttles bursts, and publishes reconciled events to the Data Bus.

### Tests for User Story 3

- [X] T029 [US3] Add `tests/integration/orchestration_fusion_test.go` covering delta+snapshot reconciliation, throttle windows, and snapshot stale handling paths.

### Implementation for User Story 3

- [X] T030 [US3] Implement conductor orchestrator in `internal/conductor/orchestrator.go` merging deltas with snapshot state, updating CAS records, and emitting fused Meltica Events.
- [X] T031 [P] [US3] Enhance in-memory snapshot store in `internal/snapshot/memory_store.go` with TTL sweeper, stale markers, and CAS retry loop instrumentation.
- [X] T032 [P] [US3] Implement throttling scheduler utilities in `internal/conductor/throttle.go` to consolidate events within configured windows and expose analytics metrics.
- [X] T033 [US3] Extend `cmd/gateway/main.go` to wire conductor orchestrator outputs, snapshot store dependencies, and publish fused events to the databus subscribers.

**Checkpoint**: All three user stories complete with independently testable outcomes.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Finalize observability, documentation, and verification across stories

- [ ] T034 [Polish] Record breaking change notes for Dispatcher/Conductor architecture in `BREAKING_CHANGES_v2.md` highlighting removed Router/Coordinator components.
- [ ] T035 [Polish] Run full smoke suite `go test ./tests/integration -run Smoke -race` and address any reliability regressions discovered.
- [ ] T036 [Polish] Add telemetry fallback unit coverage in `lib/telemetry/otel_test.go` ensuring no-op provider activates when OTLP endpoint is unset.

---

## Dependencies & Execution Order

1. **Phase 1 → Phase 2**: Setup must complete before foundational infrastructure begins.
2. **Phase 2 → User Stories**: Foundational tasks (T006–T013) block all user stories; no story work starts before completion.
3. **User Story Order**: US1 (P1) → US2 (P2) → US3 (P3). While US2 and US3 could start after Phase 2, completing US1 first provides the MVP and stabilizes shared components.
4. **Polish**: Phase 6 depends on completion of the targeted user stories.

### Story Dependency Graph

Setup → Foundational → US1 → US2 → US3 → Polish

---

## Parallel Execution Examples

- **US1**: After tests (T014–T015), run T016, T017, and T018 in parallel—they touch different files within `internal/adapters/binance/`.
- **US2**: Execute T024 first to define dispatcher control hooks, then run T025 and T027 in parallel (subscription manager vs HTTP control server) before finalizing gateway wiring with T028.
- **US3**: With T030 underway, run T031 and T032 in parallel to extend snapshot store and throttling utilities on distinct files, then integrate via T033.

---

## Implementation Strategy

1. **MVP (US1)**: Complete Phases 1–3 to deliver canonical Meltica Events end-to-end; validate via integration test T015 before proceeding.
2. **Incremental Delivery**: Layer US2 for dynamic control, ensuring SLA metrics pass, then US3 for orchestration and analytics enrichment.
3. **Quality Gates**: Keep integration tests green (T015, T023, T029) and rerun the Smoke suite (T035) before marking the feature complete.
4. **Telemetry & Backpressure**: Reuse the `lib/telemetry` and `lib/async` helpers across stories to maintain consistent observability and bounded channels.
