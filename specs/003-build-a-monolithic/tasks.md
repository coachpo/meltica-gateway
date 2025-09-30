# Tasks: Monolithic Auto-Trading Application

**Input**: Design documents from `/specs/003-build-a-monolithic/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Included as mandatory per feature specification (≥70% coverage, timeouts, race detector, IO isolation)

**Organization**: Tasks grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`
- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions
- **Project Structure**: Monolithic application at repository root
- **Providers**: `internal/adapters/{binance,coinbase,kraken}/`
- **Core**: `internal/{conductor,dispatcher,bus,consumer,schema}/`
- **Tests**: `tests/{unit,integration}/`
- **Docs**: `specs/003-build-a-monolithic/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization, documentation, and basic structure

### Specs & Diagrams

- [X] **T001** [P] [SETUP] Replace legacy UML diagrams with finalized PlantUML architecture diagram in `docs/architecture.md`
- [X] **T002** [P] [SETUP] Create Component View diagram showing Provider → Orchestrator → Dispatcher → Consumers in `docs/diagrams/component-view.puml`
- [X] **T003** [P] [SETUP] Create Canonical Data Contracts (class diagram) in `docs/diagrams/canonical-events-class.puml`
- [X] **T004** [P] [SETUP] Create Subscription Configuration sequence diagram in `docs/diagrams/subscription-seq.puml`
- [X] **T005** [P] [SETUP] Create Orderbook Assembly sequence diagram in `docs/diagrams/orderbook-assembly-seq.puml`
- [X] **T006** [P] [SETUP] Create Windowed Merge sequence diagram in `docs/diagrams/windowed-merge-seq.puml`
- [X] **T007** [P] [SETUP] Create Dispatcher Ordering sequence diagram in `docs/diagrams/dispatcher-ordering-seq.puml`
- [X] **T008** [P] [SETUP] Create Order Lifecycle state diagram in `docs/diagrams/order-lifecycle-state.puml`

### Project Infrastructure

- [X] **T009** [SETUP] Initialize Go module dependencies from `go.mod` (gorilla/websocket, golang.org/x/time/rate, testify)
- [X] **T010** [P] [SETUP] Configure golangci-lint with constitution rules (no backward-compat code paths) in `.golangci.yml`
- [X] **T011** [P] [SETUP] Create Makefile targets: `build`, `test`, `lint`, `coverage` in `Makefile`
- [X] **T012** [P] [SETUP] Setup CI workflow with ≥70% coverage enforcement in `.github/workflows/ci.yml`
- [X] **T013** [P] [SETUP] Configure per-test timeouts (5s default) and global timeout (30s) in `go test` flags

**Checkpoint**: Documentation and project infrastructure ready

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core schemas, interfaces, and buses that ALL user stories depend on

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

### Canonical Schemas

- [X] **T014** [P] [FOUND] Define canonical Event base struct with event_id, routing_version, seq_provider in `internal/schema/event.go`
- [X] **T015** [P] [FOUND] Define EventType enum (BookSnapshot, BookUpdate, Trade, Ticker, ExecReport, KlineSummary) in `internal/schema/event.go`
- [X] **T016** [P] [FOUND] Define BookSnapshotPayload and BookUpdatePayload structs in `internal/schema/event.go`
- [X] **T017** [P] [FOUND] Define TradePayload and TickerPayload structs in `internal/schema/event.go`
- [X] **T018** [P] [FOUND] Define ExecReportPayload with ExecReportState enum (ACK, PARTIAL, FILLED, CANCELLED, REJECTED, EXPIRED) in `internal/schema/event.go`
- [X] **T019** [P] [FOUND] Define OrderRequest struct with client_order_id, OrderType, TradeSide in `internal/schema/order.go`
- [X] **T020** [P] [FOUND] Define ControlMessage types (Subscribe, Unsubscribe, MergedSubscribe, SetTradingMode) in `internal/schema/control.go`

### Data Bus & Control Bus (In-Process Channels)

- [X] **T021** [FOUND] Implement in-memory Data Bus with channel-based pub/sub in `internal/bus/databus/memory.go`
- [X] **T022** [FOUND] Implement Data Bus interface (Publish, Subscribe, Unsubscribe) in `internal/bus/databus/bus.go`
- [X] **T023** [FOUND] Implement in-memory Control Bus with channel-based pub/sub in `internal/bus/controlbus/memory.go`
- [X] **T024** [FOUND] Implement Control Bus interface for command distribution in `internal/bus/controlbus/bus.go`

### Configuration

- [X] **T025** [FOUND] Extend YAML config schema for providers, orchestrator, dispatcher in `config/streaming.go`
- [X] **T026** [FOUND] Add config structs: ProviderConfig, OrchestratorConfig, DispatcherConfig, ConsumerConfig in `config/streaming.go`
- [X] **T027** [FOUND] Implement config validation (fail-fast on missing required fields) in `config/streaming.go`

### Telemetry & Observability Infrastructure

- [X] **T028** [P] [FOUND] Define TelemetryEvent schema with trace_id, decision_id in `internal/observability/telemetry.go`
- [X] **T029** [P] [FOUND] Define TelemetryEventType enum (book.resync, merge.suppressed_partial, etc.) in `internal/observability/telemetry.go`
- [X] **T030** [P] [FOUND] Implement metrics structure (buffer depth, coalesced drops, throttled ms) in `internal/observability/metrics.go`
- [X] **T031** [FOUND] Create ops-only telemetry bus (no consumer ingestion) in `internal/observability/telemetry.go`
- [X] **T032** [P] [FOUND] Implement optional DLQ for dropped events in `internal/observability/dlq.go`

### Test Infrastructure

- [X] **T033** [P] [FOUND] Create fake WebSocket server for testing in `tests/integration/fake_websocket.go`
- [X] **T034** [P] [FOUND] Create fake REST API server for testing in `tests/integration/fake_rest.go`
- [X] **T035** [P] [FOUND] Create mock provider interfaces in `tests/unit/mocks/provider.go`
- [X] **T036** [P] [FOUND] Create FakeClock for deterministic time testing in `tests/unit/fakes/clock.go`

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 1 - Real-time Market Data Consumption (Priority: P1) 🎯 MVP

**Goal**: Consumers subscribe to market data and receive time-ordered events from providers with <200ms latency

**Independent Test**: Subscribe consumer to BTC/USDT from Binance, verify order book snapshots/updates and trades arrive in correct sequence with timestamps within 200ms latency

### Tests for User Story 1

**NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [X] **T037** [P] [US1] Unit test: Provider WebSocket connection lifecycle in `tests/unit/provider_ws_test.go`
- [X] **T038** [P] [US1] Unit test: Provider REST snapshot fetcher in `tests/unit/provider_rest_test.go`
- [X] **T039** [P] [US1] Unit test: Canonical event parsing from provider-specific formats in `tests/unit/provider_parser_test.go`
- [X] **T040** [P] [US1] Unit test: Dispatcher per-stream ordering with out-of-order input in `tests/unit/stream_ordering_test.go`
- [X] **T041** [P] [US1] Integration test: End-to-end provider → orchestrator → dispatcher → consumer flow in `tests/integration/market_data_delivery_test.go`

### Implementation for User Story 1

#### Provider Module Template (Scaffold)

- [X] **T042** [P] [US1] Create provider module template with WS client interface in `internal/adapters/template/ws_client.go`
- [X] **T043** [P] [US1] Create provider module template with REST client interface in `internal/adapters/template/rest_client.go`
- [X] **T044** [P] [US1] Create provider module template with adapter interface in `internal/adapters/template/adapter.go`

#### Binance Provider (Example Implementation)

- [X] **T045** [US1] Implement Binance WebSocket client (connect, subscribe, disconnect) in `internal/adapters/binance/ws_client.go`
- [X] **T046** [US1] Implement Binance REST client for snapshots in `internal/adapters/binance/rest_client.go`
- [X] **T047** [P] [US1] Implement Binance parser (exchange format → canonical events) in `internal/adapters/binance/parser.go`
- [X] **T048** [US1] Emit canonical BookSnapshot, BookUpdate, Trade, Ticker events in `internal/adapters/binance/provider.go`

#### Orchestrator (Pass-Through Mode for US1)

- [X] **T049** [US1] Implement orchestrator with event ingestion from providers in `internal/conductor/orchestrator.go`
- [X] **T050** [US1] Implement event forwarding to dispatcher (no merge in US1) in `internal/conductor/forwarder.go`
- [X] **T051** [US1] Add routing_version stamping to forwarded events in `internal/conductor/forwarder.go`

#### Dispatcher (Core Ordering & Delivery)

- [X] **T052** [US1] Implement StreamKey (provider, symbol, eventType) in `internal/dispatcher/stream_ordering.go`
- [X] **T053** [US1] Implement StreamBuffer with EventHeap (container/heap) sorted by seq_provider in `internal/dispatcher/stream_ordering.go`
- [X] **T054** [US1] Implement flush timer (50ms) and lateness tolerance (150ms) in `internal/dispatcher/stream_ordering.go`
- [X] **T055** [US1] Implement event deduplication by event_id in `internal/dispatcher/dispatcher.go`
- [X] **T056** [US1] Implement basic routing table (no merges yet) in `internal/dispatcher/routing_table.go`
- [X] **T057** [US1] Implement event delivery to consumers via Data Bus in `internal/dispatcher/dispatcher.go`

#### Consumer (Basic)

- [X] **T058** [US1] Implement Consumer struct with consumer_id, DataBusChan in `internal/consumer/consumer.go`
- [X] **T059** [US1] Implement event receiving loop from Data Bus in `internal/consumer/consumer.go`
- [X] **T060** [US1] Add logging for received events (trace_id propagation) in `internal/consumer/consumer.go`

#### Gateway Entry Point

- [X] **T061** [US1] Create cmd/gateway/main.go with component initialization (Config → Providers → Orchestrator → Dispatcher → Consumers)
- [X] **T062** [US1] Implement graceful shutdown (context cancellation, WaitGroup) in `cmd/gateway/main.go`

**Checkpoint**: At this point, User Story 1 should be fully functional - consumers receive real-time market data with per-stream ordering

---

## Phase 4: User Story 2 - Dynamic Subscription Management (Priority: P1)

**Goal**: Consumers dynamically subscribe/unsubscribe via control bus, routing updates happen within 1 second

**Independent Test**: Send subscribe/unsubscribe control commands for various symbols, verify routing table updates and event delivery starts/stops within 1 second

### Tests for User Story 2

- [X] **T063** [P] [US2] Unit test: Control message parsing (Subscribe, Unsubscribe) in `tests/unit/control_message_test.go`
- [X] **T064** [P] [US2] Unit test: Routing table updates (add/remove subscriptions) in `tests/unit/routing_table_test.go`
- [X] **T065** [P] [US2] Integration test: Subscribe → route events → unsubscribe → stop events in `tests/integration/subscription_management_test.go`

### Implementation for User Story 2

#### Control Bus Integration

- [X] **T066** [US2] Implement control message handler in dispatcher (Subscribe, Unsubscribe) in `internal/dispatcher/control_http.go`
- [X] **T067** [US2] Implement routing table updates from control messages in `internal/dispatcher/routing_table.go`
- [X] **T068** [US2] Increment routing_version on table updates in `internal/dispatcher/routing_table.go`
- [X] **T069** [US2] Implement control acknowledgement responses (success/error) in `internal/dispatcher/control_http.go`

#### Subscription State Management

- [X] **T070** [US2] Implement Subscription struct (subscription_id, consumer_id, symbol, providers, active) in `internal/consumer/subscription_manager.go`
- [X] **T071** [US2] Implement SubscriptionManager (track active subscriptions per consumer) in `internal/consumer/subscription_manager.go`
- [X] **T072** [US2] Implement idempotent subscribe (duplicate commands don't create duplicates) in `internal/consumer/subscription_manager.go`

#### Provider Dynamic Subscription

- [X] **T073** [US2] Implement dynamic symbol subscription in provider WebSocket clients in `internal/adapters/binance/ws_client.go`
- [X] **T074** [US2] Implement unsubscribe logic (close streams without disconnecting WS) in `internal/adapters/binance/ws_client.go`

**Checkpoint**: At this point, User Stories 1 AND 2 should both work independently - consumers can dynamically manage subscriptions

---

## Phase 5: User Story 4 - Order Submission with Trading Switch Control (Priority: P1)

**Goal**: Consumers submit orders with idempotent client_order_id, trading switch controls order suppression

**Independent Test**: Enable trading switch, submit order with unique client_order_id, verify order reaches provider once; disable switch and confirm subsequent orders suppressed

**Note**: Implementing US4 before US3 as US4 is P1 and foundational for trading functionality

### Tests for User Story 4

- [ ] **T075** [P] [US4] Unit test: Trading switch enable/disable in `tests/unit/trading_switch_test.go`
- [ ] **T076** [P] [US4] Unit test: client_order_id deduplication in `tests/unit/idempotent_orders_test.go`
- [ ] **T077** [P] [US4] Integration test: Order submission → provider → idempotency check in `tests/integration/order_submission_test.go`

### Implementation for User Story 4

#### Trading Switch

- [ ] **T078** [P] [US4] Implement TradingSwitch struct (consumer_id, enabled) in `internal/consumer/trading_switch.go`
- [ ] **T079** [US4] Implement CheckEnabled method and control bus listener in `internal/consumer/trading_switch.go`
- [ ] **T080** [US4] Implement SetTradingMode control message handler in `internal/consumer/consumer.go`

#### Order Request Flow

- [ ] **T081** [US4] Implement OrderRequest validation (required fields, client_order_id format) in `internal/dispatcher/order_handler.go`
- [ ] **T082** [US4] Implement client_order_id deduplication map (in-memory, 1-hour retention) in `internal/dispatcher/order_handler.go`
- [ ] **T083** [US4] Route order to appropriate provider adapter in `internal/dispatcher/order_handler.go`

#### Provider Order Submission

- [ ] **T084** [US4] Implement Binance order submission adapter (REST API call) in `internal/adapters/binance/order_adapter.go`
- [ ] **T085** [US4] Implement provider-level client_order_id deduplication (exactly-once guarantee) in `internal/adapters/binance/order_adapter.go`
- [ ] **T086** [US4] Add telemetry for duplicate order detection in `internal/adapters/binance/order_adapter.go`

#### Consumer Order Submission

- [ ] **T087** [US4] Implement consumer SubmitOrder method with trading switch check in `internal/consumer/consumer.go`
- [ ] **T088** [US4] Suppress order locally if trading switch disabled (no dispatcher submission) in `internal/consumer/consumer.go`

**Checkpoint**: At this point, User Stories 1, 2, AND 4 should all work independently - orders can be submitted with idempotency and trading switch control

---

## Phase 6: User Story 5 - Execution Report Delivery (Priority: P1)

**Goal**: Consumers receive all execution reports (ACK, PARTIAL, FILLED, etc.) losslessly, never coalesced

**Independent Test**: Submit order, have provider emit ACK → PARTIAL → FILLED, verify all reports reach consumer in order without loss

### Tests for User Story 5

- [ ] **T089** [P] [US5] Unit test: ExecReport canonical parsing from provider formats in `tests/unit/execreport_parser_test.go`
- [ ] **T090** [P] [US5] Unit test: ExecReport never coalesced under backpressure in `tests/unit/coalescing_test.go`
- [ ] **T091** [P] [US5] Integration test: Full order lifecycle (submit → ACK → PARTIAL → FILLED) in `tests/integration/order_lifecycle_test.go`

### Implementation for User Story 5

#### Provider ExecReport Normalization

- [ ] **T092** [US5] Implement Binance ExecReport WebSocket listener in `internal/adapters/binance/ws_client.go`
- [ ] **T093** [US5] Parse Binance execution reports into canonical ExecReportPayload in `internal/adapters/binance/parser.go`
- [ ] **T094** [US5] Map Binance order states to ExecReportState enum (ACK, PARTIAL, FILLED, CANCELLED, REJECTED, EXPIRED) in `internal/adapters/binance/parser.go`

#### Lossless ExecReport Path

- [ ] **T095** [US5] Mark ExecReport as non-coalescable in EventType.Coalescable() in `internal/schema/event.go`
- [ ] **T096** [US5] Implement ExecReport priority delivery (bypass coalescing, never drop) in `internal/dispatcher/coalescer.go`
- [ ] **T097** [US5] Add ExecReport backpressure handling (queue without dropping) in `internal/dispatcher/backpressure.go`

#### Consumer ExecReport Monitoring

- [ ] **T098** [US5] Implement ExecReport handler in consumer (track order state) in `internal/consumer/consumer.go`
- [ ] **T099** [US5] Log all ExecReport state transitions with trace_id in `internal/consumer/consumer.go`

**Checkpoint**: At this point, User Stories 1, 2, 4, AND 5 should all work independently - complete trading lifecycle with lossless execution reports

---

## Phase 7: User Story 6 - Provider-Side Order Book Assembly (Priority: P2)

**Goal**: Providers assemble accurate order books using snapshot+diff with checksum verification and periodic refresh

**Independent Test**: Simulate WebSocket stream with sequenced updates, cause gap/checksum mismatch, verify system detects issue and triggers REST snapshot refresh

### Tests for User Story 6

- [ ] **T100** [P] [US6] Unit test: Order book gap detection (seq 100, 101, 103) in `tests/unit/book_assembler_test.go`
- [ ] **T101** [P] [US6] Unit test: Checksum verification and resync trigger in `tests/unit/book_assembler_test.go`
- [ ] **T102** [P] [US6] Unit test: Staleness detection and proactive refresh in `tests/unit/book_assembler_test.go`
- [ ] **T103** [P] [US6] Integration test: Full book assembly cycle (snapshot → diffs → gap → resync) in `tests/integration/orderbook_assembly_test.go`

### Implementation for User Story 6

#### Book Assembler Interface

- [ ] **T104** [P] [US6] Define BookAssembler interface (ApplyDiff, VerifyChecksum, RefreshSnapshot) in `internal/adapters/template/book_assembler.go`
- [ ] **T105** [P] [US6] Define OrderBook struct (provider, symbol, seq, checksum, bids, asks) in `internal/adapters/template/book_assembler.go`

#### Binance Book Assembler

- [ ] **T106** [US6] Implement initial REST snapshot fetcher in `internal/adapters/binance/book_assembler.go`
- [ ] **T107** [US6] Implement WebSocket diff buffer (apply by sequence number) in `internal/adapters/binance/book_assembler.go`
- [ ] **T108** [US6] Implement gap detection (missing sequence numbers) in `internal/adapters/binance/book_assembler.go`
- [ ] **T109** [US6] Implement Binance CRC32 checksum verification in `internal/adapters/binance/book_assembler.go`
- [ ] **T110** [US6] Implement periodic refresh timer (2-5 minutes, configurable) in `internal/adapters/binance/book_assembler.go`
- [ ] **T111** [US6] Implement staleness detection (no updates in 5 minutes) in `internal/adapters/binance/book_assembler.go`
- [ ] **T112** [US6] Trigger REST snapshot refresh on gap/checksum/staleness in `internal/adapters/binance/book_assembler.go`

#### Telemetry for Book Assembly

- [ ] **T113** [US6] Emit book.resync telemetry event (provider, symbol, reason) in `internal/adapters/binance/book_assembler.go`
- [ ] **T114** [US6] Emit checksum.failed telemetry event in `internal/adapters/binance/book_assembler.go`

**Checkpoint**: At this point, User Stories 1, 2, 4, 5, AND 6 should all work independently - high-quality order books with automatic error recovery

---

## Phase 8: User Story 3 - Merged Multi-Provider Data Streams (Priority: P2)

**Goal**: Consumers subscribe to merged data from multiple providers with windowed merge logic (late-drop, partial-suppress)

**Independent Test**: Configure merged subscription for symbol on 3 providers, send events with varying latencies, verify only complete windows with all providers are emitted

**Note**: Implemented after US1/2/4/5/6 as US3 is P2 and builds on existing functionality

### Tests for User Story 3

- [ ] **T115** [P] [US3] Unit test: Merge window opening on first event in `tests/unit/merge_window_test.go`
- [ ] **T116** [P] [US3] Unit test: Window closure by time (10s) or count (1000) in `tests/unit/merge_window_test.go`
- [ ] **T117** [P] [US3] Unit test: Late fragment drop (arrives after window close) in `tests/unit/merge_window_test.go`
- [ ] **T118** [P] [US3] Unit test: Partial window suppression (missing providers) in `tests/unit/merge_window_test.go`
- [ ] **T119** [P] [US3] Integration test: Complete merged window (all providers present) in `tests/integration/windowed_merge_test.go`

### Implementation for User Story 3

#### Merge Window Engine

- [ ] **T120** [US3] Implement MergeWindow struct (merge_id, open_time, expected_providers, received_events) in `internal/conductor/merge_window.go`
- [ ] **T121** [US3] Implement MergeKey (symbol, eventType) and window index in `internal/conductor/merge_window.go`
- [ ] **T122** [US3] Implement window opening on first fragment arrival in `internal/conductor/orchestrator.go`
- [ ] **T123** [US3] Implement time-based closure (10-second timer with time.Ticker) in `internal/conductor/orchestrator.go`
- [ ] **T124** [US3] Implement count-based closure (1000-event threshold) in `internal/conductor/orchestrator.go`
- [ ] **T125** [US3] Implement late fragment detection and drop logic in `internal/conductor/orchestrator.go`
- [ ] **T126** [US3] Implement completeness check (all expected providers present) in `internal/conductor/orchestrator.go`
- [ ] **T127** [US3] Emit merged event with merge_id when window complete in `internal/conductor/orchestrator.go`
- [ ] **T128** [US3] Suppress partial window (missing providers) and emit telemetry in `internal/conductor/orchestrator.go`

#### Merge Configuration

- [ ] **T129** [US3] Implement MergedSubscribe control message handler in `internal/dispatcher/control_http.go`
- [ ] **T130** [US3] Propagate merge config (keys, window params) to orchestrator via Control Bus in `internal/conductor/orchestrator.go`
- [ ] **T131** [US3] Update routing table with merged subscription signatures in `internal/dispatcher/routing_table.go`

#### Merged Event Delivery

- [ ] **T132** [US3] Implement merged event routing (merge_id in event) in `internal/dispatcher/dispatcher.go`
- [ ] **T133** [US3] Deliver merged events to subscribed consumers in `internal/dispatcher/dispatcher.go`

#### Telemetry for Merging

- [ ] **T134** [US3] Emit merge.suppressed_partial telemetry (merge_key, missing_providers) in `internal/conductor/orchestrator.go`
- [ ] **T135** [US3] Emit late_event.dropped telemetry (provider, symbol, seq_provider, lateness_ms) in `internal/conductor/orchestrator.go`

**Checkpoint**: At this point, User Stories 1-6 should all work independently - advanced merged multi-provider streams available

---

## Phase 9: User Story 7 - Fair-Share Bandwidth Management (Priority: P3)

**Goal**: Dispatcher applies fair-share token-bucket rate limiting, coalesces market data under backpressure, never drops ExecReports

**Independent Test**: Run 3 consumers on high-volume streams, simulate congestion, verify fair bandwidth allocation with ExecReports preserved

### Tests for User Story 7

- [ ] **T136** [P] [US7] Unit test: Token-bucket rate limiting (golang.org/x/time/rate) in `tests/unit/backpressure_test.go`
- [ ] **T137** [P] [US7] Unit test: Latest-wins coalescing for Ticker/Book/KlineSummary in `tests/unit/coalescing_test.go`
- [ ] **T138** [P] [US7] Unit test: Fair-share allocation across multiple consumers in `tests/unit/fair_share_test.go`
- [ ] **T139** [P] [US7] Integration test: Backpressure scenario with 3 consumers, high load in `tests/integration/backpressure_test.go`

### Implementation for User Story 7

#### Token-Bucket Rate Limiting

- [ ] **T140** [US7] Implement StreamLimiter with rate.Limiter per stream in `internal/dispatcher/backpressure.go`
- [ ] **T141** [US7] Configure token rate (1000 events/sec) and burst (100) from config in `internal/dispatcher/backpressure.go`
- [ ] **T142** [US7] Check limiter.Allow() before event dispatch in `internal/dispatcher/backpressure.go`

#### Coalescing Logic

- [ ] **T143** [US7] Implement Coalescer with map[StreamKey]*Event in `internal/dispatcher/coalescer.go`
- [ ] **T144** [US7] Mark coalescable types (Ticker, BookUpdate, KlineSummary) in config in `internal/dispatcher/coalescer.go`
- [ ] **T145** [US7] Implement latest-wins replacement (drop old, keep latest) in `internal/dispatcher/coalescer.go`
- [ ] **T146** [US7] Flush coalesced events on 50ms timer in `internal/dispatcher/coalescer.go`
- [ ] **T147** [US7] Ensure ExecReport NEVER enters coalescer (bypass entirely) in `internal/dispatcher/coalescer.go`

#### Fair-Share Scheduler

- [ ] **T148** [US7] Implement fair-share fan-out (round-robin across consumers) in `internal/dispatcher/dispatcher.go`
- [ ] **T149** [US7] Track per-consumer bandwidth usage in `internal/dispatcher/dispatcher.go`
- [ ] **T150** [US7] Apply backpressure when consumer's token bucket depleted in `internal/dispatcher/backpressure.go`

#### Telemetry for Backpressure

- [ ] **T151** [US7] Emit coalescing.applied telemetry (stream_key, dropped_count) in `internal/dispatcher/coalescer.go`
- [ ] **T152** [US7] Emit backpressure.triggered telemetry (stream_key, buffer_depth) in `internal/dispatcher/backpressure.go`

**Checkpoint**: All user stories (US1-US7) should now work independently - complete trading monolith with fair-share backpressure

---

## Phase 10: Additional Providers (Coinbase, Kraken)

**Purpose**: Extend provider support using scaffolded template

- [ ] **T153** [P] [EXTEND] Implement Coinbase WebSocket client (following Binance pattern) in `internal/adapters/coinbase/ws_client.go`
- [ ] **T154** [P] [EXTEND] Implement Coinbase REST client in `internal/adapters/coinbase/rest_client.go`
- [ ] **T155** [P] [EXTEND] Implement Coinbase parser (exchange format → canonical) in `internal/adapters/coinbase/parser.go`
- [ ] **T156** [P] [EXTEND] Implement Coinbase book assembler (SHA256 checksum) in `internal/adapters/coinbase/book_assembler.go`
- [ ] **T157** [P] [EXTEND] Implement Coinbase order submission adapter in `internal/adapters/coinbase/order_adapter.go`

- [ ] **T158** [P] [EXTEND] Implement Kraken WebSocket client in `internal/adapters/kraken/ws_client.go`
- [ ] **T159** [P] [EXTEND] Implement Kraken REST client in `internal/adapters/kraken/rest_client.go`
- [ ] **T160** [P] [EXTEND] Implement Kraken parser (exchange format → canonical) in `internal/adapters/kraken/parser.go`
- [ ] **T161** [P] [EXTEND] Implement Kraken book assembler in `internal/adapters/kraken/book_assembler.go`
- [ ] **T162** [P] [EXTEND] Implement Kraken order submission adapter in `internal/adapters/kraken/order_adapter.go`

**Checkpoint**: All 3 providers (Binance, Coinbase, Kraken) operational

---

## Phase 11: Observability & Telemetry

**Purpose**: Complete observability stack for ops-only monitoring

- [ ] **T163** [P] [OBS] Implement Prometheus metrics exporter on port 9090 in `internal/observability/metrics.go`
- [ ] **T164** [P] [OBS] Add buffer depth metrics per stream in `internal/observability/metrics.go`
- [ ] **T165** [P] [OBS] Add coalesced drops counter in `internal/observability/metrics.go`
- [ ] **T166** [P] [OBS] Add throttled milliseconds counter in `internal/observability/metrics.go`
- [ ] **T167** [P] [OBS] Add suppressed partials counter in `internal/observability/metrics.go`
- [ ] **T168** [P] [OBS] Add book resyncs counter per provider in `internal/observability/metrics.go`
- [ ] **T169** [P] [OBS] Add websocket reconnects counter per provider in `internal/observability/metrics.go`

- [ ] **T170** [P] [OBS] Implement structured logging with trace_id/decision_id propagation in `internal/observability/logger.go`
- [ ] **T171** [P] [OBS] Configure log levels (info, warn, error) in `internal/observability/logger.go`

- [ ] **T172** [P] [OBS] Integrate OpenTelemetry tracing (use existing lib/telemetry/otel.go) in `cmd/gateway/main.go`
- [ ] **T173** [P] [OBS] Propagate trace_id through event pipeline in `internal/schema/event.go`

**Checkpoint**: Full observability stack operational

---

## Phase 12: Quality Assurance & CI Enforcement

**Purpose**: Enforce ≥70% coverage, timeouts, race detection, and no backward-compat code

### Coverage Enforcement

- [ ] **T174** [QA] Configure `go test -coverprofile=coverage.out` in CI workflow in `.github/workflows/ci.yml`
- [ ] **T175** [QA] Add coverage threshold check: fail build if <70% in `.github/workflows/ci.yml`
- [ ] **T176** [QA] Generate coverage report: `go tool cover -html=coverage.out` in CI

### Timeout Enforcement

- [ ] **T177** [QA] Add per-test timeout flag: `-timeout=30s` in Makefile `test` target
- [ ] **T178** [QA] Wrap all test contexts with `context.WithTimeout(5s)` in test files
- [ ] **T179** [QA] Add global CI timeout: `timeout-minutes: 15` in `.github/workflows/ci.yml`

### Race Detection

- [ ] **T180** [QA] Ensure all tests run with `-race` flag in Makefile and CI
- [ ] **T181** [QA] Fix any race conditions detected by tests

### Mocks/Fakes (No Live Network)

- [ ] **T182** [QA] Verify all unit tests use mocks/fakes (no real WebSocket/REST calls)
- [ ] **T183** [QA] Add CI check: fail if unit tests make network calls (lint rule or static analysis)

### Ban Backward-Compat Code Paths

- [ ] **T184** [QA] Add golangci-lint rule: fail on deprecated API usage in `.golangci.yml`
- [ ] **T185** [QA] Add CI check: grep for "legacy", "deprecated", "shim", "feature_flag" → fail build
- [ ] **T186** [QA] Add code review checklist: "No backward-compatibility code allowed"

### Static Checks

- [ ] **T187** [QA] Run `golangci-lint run` in CI (already configured in T010)
- [ ] **T188** [QA] Enforce gofmt and goimports in CI
- [ ] **T189** [QA] Run `go vet` in CI

**Checkpoint**: Quality gates enforced - all builds must pass 70% coverage, timeouts, race detector, and no backward-compat

---

## Phase 13: Polish & Cross-Cutting Concerns

**Purpose**: Final improvements affecting multiple user stories

- [ ] **T190** [P] [POLISH] Create end-to-end integration test covering all user stories in `tests/integration/end_to_end_all_stories_test.go`
- [ ] **T191** [P] [POLISH] Performance profiling: measure latency (target <200ms p99) using `go test -bench`
- [ ] **T192** [P] [POLISH] Validate quickstart.md instructions (manual walkthrough)
- [ ] **T193** [P] [POLISH] Update README.md with architecture diagram and getting started guide
- [ ] **T194** [P] [POLISH] Generate API documentation from OpenAPI contracts
- [ ] **T195** [P] [POLISH] Security audit: check for credential leaks, validate input sanitization
- [ ] **T196** [P] [POLISH] Add graceful degradation tests (single provider failure shouldn't crash monolith)
- [ ] **T197** [P] [POLISH] Add reconnection tests (WebSocket disconnect → exponential backoff → reconnect)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phases 3-9)**: All depend on Foundational phase completion
  - User stories can then proceed in parallel (if staffed)
  - Or sequentially in priority order (P1 → P2 → P3)
- **Additional Providers (Phase 10)**: Can start after US1 (provider template exists)
- **Observability (Phase 11)**: Can start after Foundational, parallel with user stories
- **QA & CI (Phase 12)**: Continuous throughout, finalized after all user stories
- **Polish (Phase 13)**: Depends on all desired user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 2 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories (independent)
- **User Story 3 (P2)**: Depends on US1 (needs basic event flow) and US2 (needs subscription management)
- **User Story 4 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories (independent)
- **User Story 5 (P1)**: Depends on US4 (needs order submission path)
- **User Story 6 (P2)**: Can start after Foundational (Phase 2) - Enhances US1 but independent
- **User Story 7 (P3)**: Depends on US1 (needs event dispatch infrastructure)

### Within Each User Story

- Tests MUST be written and FAIL before implementation (TDD)
- Models/schemas before services
- Services before handlers
- Core implementation before integration
- Story complete before moving to next priority

### Parallel Opportunities

- All Setup tasks (T001-T013) marked [P] can run in parallel
- All Foundational schemas (T014-T020) marked [P] can run in parallel
- Once Foundational phase completes:
  - US1, US2, US4, US6 can all start in parallel (independent)
  - US3 can start after US1+US2 complete
  - US5 can start after US4 completes
  - US7 can start after US1 completes
- All tests within a user story marked [P] can run in parallel
- Providers (Binance, Coinbase, Kraken) marked [P] can be implemented in parallel
- Observability tasks marked [P] can run in parallel

---

## Parallel Example: User Story 1 (Market Data Consumption)

```bash
# Launch all tests for User Story 1 together (TDD - write first, ensure FAIL):
Task: "Unit test: Provider WebSocket connection lifecycle"
Task: "Unit test: Provider REST snapshot fetcher"
Task: "Unit test: Canonical event parsing"
Task: "Unit test: Dispatcher per-stream ordering"
Task: "Integration test: End-to-end market data delivery"

# After tests fail, launch all provider template tasks together:
Task: "Create provider module template with WS client interface"
Task: "Create provider module template with REST client interface"
Task: "Create provider module template with adapter interface"

# Implement Binance provider sequentially (dependencies):
Task: "Implement Binance WebSocket client"
Task: "Implement Binance REST client"
Task: "Implement Binance parser" [P with REST client]
Task: "Emit canonical events"
```

---

## Parallel Example: Multiple User Stories After Foundational

```bash
# Team A: User Story 1 (Market Data Consumption)
# Team B: User Story 2 (Subscription Management)
# Team C: User Story 4 (Order Submission)
# Team D: User Story 6 (Book Assembly)

# All four teams can work in parallel after Phase 2 (Foundational) completes

# Once US1 + US2 complete:
# Team A: User Story 3 (Merged Streams)

# Once US4 completes:
# Team C: User Story 5 (Execution Reports)

# Once US1 completes:
# Team A: User Story 7 (Fair-Share Backpressure)
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (diagrams, project structure)
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1 (market data consumption)
4. **STOP and VALIDATE**: Test US1 independently (subscribe → receive events)
5. Deploy/demo if ready

**Time Estimate**: 2-3 weeks for experienced Go developer

### Incremental Delivery (P1 Stories)

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test independently → Deploy/Demo (MVP!)
3. Add User Story 2 → Test independently → Deploy/Demo (dynamic subscriptions)
4. Add User Story 4 → Test independently → Deploy/Demo (order submission)
5. Add User Story 5 → Test independently → Deploy/Demo (execution reports)
6. **P1 Feature-Complete**: All critical trading functionality operational

**Time Estimate**: 4-6 weeks for experienced Go developer

### Full Feature Set (All Stories P1-P3)

1. Complete P1 stories (US1, US2, US4, US5)
2. Add User Story 6 (book assembly) → Deploy/Demo
3. Add User Story 3 (merged streams) → Deploy/Demo
4. Add User Story 7 (fair-share backpressure) → Deploy/Demo
5. Add additional providers (Coinbase, Kraken)
6. Finalize observability and QA enforcement

**Time Estimate**: 8-12 weeks for experienced Go developer

### Parallel Team Strategy

With multiple developers:

1. **Week 1-2**: Team completes Setup + Foundational together
2. **Week 3-4**: Once Foundational is done:
   - Developer A: User Story 1 (market data)
   - Developer B: User Story 2 (subscriptions)
   - Developer C: User Story 4 (orders)
   - Developer D: User Story 6 (book assembly)
3. **Week 5**: Stories complete and integrate independently
4. **Week 6**: Developer A adds US3 (merged streams), Developer C adds US5 (exec reports)
5. **Week 7-8**: Developer A adds US7 (backpressure), team adds additional providers
6. **Week 9-10**: Polish, observability, QA enforcement

**Time Estimate**: 8-10 weeks with 4 developers

---

## Notes

- **[P] tasks**: Different files, no dependencies - can run in parallel
- **[Story] label**: Maps task to specific user story for traceability
- **Each user story**: Independently completable and testable
- **Verify tests fail**: Before implementing (TDD approach)
- **Commit strategy**: After each task or logical group
- **Checkpoints**: Stop to validate story independently
- **Avoid**: Vague tasks, same file conflicts, cross-story dependencies that break independence

### Constitution Compliance Reminders

- **LM-01**: Maintain immutable boundaries: Providers → Orchestrator → Dispatcher → Consumers
- **LM-02**: Use canonical, versioned event schemas; merged events are first-class
- **LM-03**: Enforce per-stream ordering with seq_provider buffer + ingest_ts fallback
- **LM-04**: Latest-wins coalescing for market data; NEVER drop ExecReport
- **LM-05**: Windowed merge: open on first, close by time/count, late=drop, partial=suppress
- **LM-06**: Idempotent orders via client_order_id; lossless ExecReport path
- **LM-07**: Provider-side book assembly: snapshot+diff, checksums, periodic refresh
- **LM-08**: Ops-only telemetry with trace_id/decision_id; consumers don't ingest telemetry
- **LM-09**: Discard in-flight merges on restart; no replay
- **CQ-08/GOV-04**: NO backward compatibility coding; break APIs freely
- **TS-01-05**: ≥70% coverage, timeouts, -race, IO isolation in tests
- **ARCH-01/02**: Keep reusable infrastructure in `/lib`

---

## Task Summary

- **Total Tasks**: 197
- **Setup Phase**: 13 tasks
- **Foundational Phase**: 23 tasks (CRITICAL - blocks all stories)
- **User Story 1 (P1)**: 25 tasks (market data consumption)
- **User Story 2 (P1)**: 12 tasks (subscription management)
- **User Story 4 (P1)**: 14 tasks (order submission)
- **User Story 5 (P1)**: 10 tasks (execution reports)
- **User Story 6 (P2)**: 15 tasks (book assembly)
- **User Story 3 (P2)**: 21 tasks (merged streams)
- **User Story 7 (P3)**: 17 tasks (fair-share backpressure)
- **Additional Providers**: 10 tasks (Coinbase, Kraken)
- **Observability**: 11 tasks
- **QA & CI Enforcement**: 16 tasks
- **Polish**: 8 tasks

**Parallel Opportunities**: 89 tasks marked [P] (45% can run in parallel)

**MVP Scope** (User Story 1 only): 36 tasks (Setup + Foundational + US1)

**P1 Feature-Complete** (US1, US2, US4, US5): 97 tasks

**Suggested Starting Point**: Complete Setup + Foundational phases, then begin User Story 1 for fastest path to working MVP.

