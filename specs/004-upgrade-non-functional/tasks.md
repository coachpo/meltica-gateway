# Tasks: Performance & Memory Architecture Upgrade

**Input**: Design documents from `/specs/004-upgrade-non-functional/`
**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: Tests included per specification requirements (unit tests for Reset(), integration tests for pooling, benchmarks for performance validation)

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`
- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3, US4)
- Include exact file paths in descriptions

## Path Conventions
- **Go monolithic project**: `internal/`, `lib/`, `tests/` at repository root
- All paths are absolute or relative to repository root

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and dependency management

- [X] T001 [P] Add new dependencies to go.mod: github.com/coder/websocket and github.com/goccy/go-json
- [X] T002 [P] Run go mod tidy to update go.sum and remove unused gorilla/websocket dependency
- [X] T003 [P] Create internal/pool/ package directory for pool management utilities
- [X] T004 [P] Create lib/pool/ package directory for reusable pool patterns (if needed)

**Checkpoint**: Dependencies installed, package structure ready

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

### Pool Infrastructure

- [X] T005 [P] Implement BoundedPool wrapper in internal/pool/bounded.go with semaphore-based capacity control
- [X] T006 [P] Implement PoolManager coordinator in internal/pool/manager.go with WaitGroup tracking for shutdown
- [X] T007 [P] Implement double-Put() detection logic in internal/pool/lifecycle.go with panic on violation
- [X] T008 Add pooled object interface (SetReturned, IsReturned) to internal/pool/interface.go

### Pooled Struct Updates

- [X] T009 [P] Add `returned bool` field and Reset() method to schema.WsFrame in internal/schema/frame.go
- [X] T010 [P] Add `returned bool` field and Reset() method to schema.ProviderRaw in internal/schema/provider.go
- [X] T011 [P] Add `returned bool` field and Reset() method to schema.CanonicalEvent in internal/schema/event.go
- [X] T012 [P] Add `returned bool` field and Reset() method to schema.MergedEvent in internal/schema/merge.go
- [X] T013 [P] Add `returned bool` field and Reset() method to schema.OrderRequest in internal/schema/order.go
- [X] T014 [P] Add `returned bool` field and Reset() method to schema.ExecReport in internal/schema/order.go

### Pool Registration

- [X] T015 Register all 6 pools (WsFrame, ProviderRaw, CanonicalEvent, MergedEvent, OrderRequest, ExecReport) in cmd/gateway/main.go during startup

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 1 - Reduced Memory Footprint Under Load (Priority: P1) 🎯 MVP

**Goal**: System maintains stable memory usage and avoids GC pressure through object pooling

**Independent Test**: Deploy with pooled structs, run sustained load (50 symbols, 3 providers, 30 min), verify 40% allocation reduction and GC pause <10ms p99

### Unit Tests for US1

**NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [X] T016 [P] [US1] Unit test for WsFrame.Reset() in tests/unit/pool_lifecycle_test.go - verify all fields zeroed
- [X] T017 [P] [US1] Unit test for ProviderRaw.Reset() in tests/unit/pool_lifecycle_test.go - verify all fields zeroed
- [X] T018 [P] [US1] Unit test for CanonicalEvent.Reset() in tests/unit/pool_lifecycle_test.go - verify all fields zeroed
- [X] T019 [P] [US1] Unit test for MergedEvent.Reset() in tests/unit/pool_lifecycle_test.go - verify all fields zeroed
- [X] T020 [P] [US1] Unit test for OrderRequest.Reset() in tests/unit/pool_lifecycle_test.go - verify all fields zeroed
- [X] T021 [P] [US1] Unit test for ExecReport.Reset() in tests/unit/pool_lifecycle_test.go - verify all fields zeroed
- [X] T022 [P] [US1] Unit test for double-Put() panic in tests/unit/pool_lifecycle_test.go - verify panic with stack trace
- [X] T023 [P] [US1] Unit test for pool exhaustion and 100ms timeout in tests/unit/pool_lifecycle_test.go
  - Setup: Exhaust pool capacity (acquire all semaphore slots)
  - Action: Call Get(ctx) with context.WithTimeout(ctx, 100*time.Millisecond)
  - Assert: Returns (nil, context.DeadlineExceeded) after ~100ms
  - Verify: Timeout duration matches pool Get() specification (100ms per FR-008)

### Provider Path (WS Frame Pooling)

- [X] T024 [US1] Update internal/adapters/binance/ws_client.go readLoop() to Get() pooled WsFrame with 100ms timeout
- [X] T025 [US1] Update internal/adapters/binance/ws_client.go readLoop() to defer Put() WsFrame after parsing
- [X] T026 [US1] Update internal/adapters/binance/parser.go to Get() pooled ProviderRaw with 100ms timeout
- [X] T027 [US1] Update internal/adapters/binance/parser.go to defer Put() ProviderRaw after normalization

### Canonical Event Pooling

- [X] T028 [US1] Update internal/adapters/binance/parser.go normalize() to Get() pooled CanonicalEvent with 100ms timeout
- [X] T029 [US1] Update internal/adapters/binance/parser.go normalize() to defer Put() CanonicalEvent after dispatch

### Orchestrator Merge Pooling

- [X] T030 [US1] Update internal/conductor/orchestrator_v2.go to Get() pooled MergedEvent when window closes
- [X] T031 [US1] Update internal/conductor/orchestrator_v2.go to Put() MergedEvent after successful handoff to Dispatcher

### Dispatcher Fan-Out (Unpooled Clones)

- [X] T032 [US1] Implement heap clone allocation in internal/dispatcher/stream_ordering.go fanOut() method
- [X] T033 [US1] Add deep-copy logic for mutable slices (Data field) in internal/dispatcher/stream_ordering.go clone()
- [X] T034 [US1] Add shallow-copy logic for immutable fields (Provider, Symbol, timestamps) in clone()
- [X] T035 [US1] Update internal/dispatcher/stream_ordering.go to Put() original pooled event after all clones enqueued

### Consumer Updates

- [X] T036 [P] [US1] Update internal/consumer/consumer.go handlers to process heap clones without calling Put()
- [X] T036 [P] [US1] Update internal/consumer/consumer.go handlers to process heap clones without calling Put()
- [X] T037 [P] [US1] Add documentation comments in internal/consumer/consumer.go explaining clone ownership (no Put() needed)

### Integration Tests for US1

- [X] T038 [US1] Integration test in tests/integration/pooling_e2e_test.go - send 1000 events, verify Get/Put balanced
- [X] T039 [US1] Integration test in tests/integration/pooling_e2e_test.go - verify no panics from double-Put()
- [X] T040 [US1] Integration test in tests/integration/pooling_e2e_test.go - verify 40% allocation reduction vs baseline

**Checkpoint**: At this point, User Story 1 should be fully functional with pooling delivering memory footprint reduction

---

## Phase 4: User Story 2 - Improved Message Processing Latency (Priority: P1)

**Goal**: Minimize end-to-end latency through efficient serialization (coder/websocket, goccy/go-json)

**Independent Test**: Run latency benchmarks, verify p99 <150ms end-to-end and 30% faster JSON parsing

### Unit Tests for US2

- [X] T041 [P] [US2] Unit test in tests/unit/websocket_migration_test.go - verify coder/websocket Read() behavior equivalence
- [X] T042 [P] [US2] Unit test in tests/unit/websocket_migration_test.go - verify coder/websocket context timeout handling
- [X] T043 [P] [US2] Unit test in tests/unit/json_migration_test.go - verify goccy/go-json Marshal() behavior equivalence
- [X] T044 [P] [US2] Unit test in tests/unit/json_migration_test.go - verify goccy/go-json Unmarshal() behavior equivalence

### WebSocket Migration (coder/websocket)

- [X] T045 [US2] Replace gorilla/websocket Dial with coder/websocket.Dial in internal/adapters/binance/ws_client.go
- [X] T046 [US2] Update internal/adapters/binance/ws_client.go to pass context.Context to Read() instead of SetReadDeadline()
- [X] T047 [US2] Update internal/adapters/binance/ws_client.go to pass context.Context to Write() operations
- [X] T048 [US2] Update internal/adapters/binance/ws_client.go Close() to use coder/websocket.StatusCode pattern
- [X] T049 [US2] Implement ping/pong via coder/websocket.Conn.Ping(ctx) in internal/adapters/binance/ws_client.go

### JSON Migration (goccy/go-json)

- [X] T050 [US2] Replace encoding/json import with goccy/go-json in internal/adapters/binance/parser.go
- [X] T051 [US2] Replace encoding/json import with goccy/go-json in internal/dispatcher/control_http.go
- [X] T052 [US2] Replace encoding/json import with goccy/go-json in internal/schema/event.go (if custom marshalers exist)
- [X] T053 [US2] Replace encoding/json import with goccy/go-json in internal/consumer/consumer.go
- [X] T054 [US2] Add pooled Encoder/Decoder helpers in internal/pool/json_helpers.go for repeated JSON ops

### Benchmarks for US2

- [X] T055 [P] [US2] Benchmark in tests/unit/websocket_migration_test.go - compare coder vs gorilla frame overhead
- [X] T056 [P] [US2] Benchmark in tests/unit/json_migration_test.go - verify 30% speedup for Marshal/Unmarshal
- [X] T057 [US2] End-to-end latency benchmark in tests/integration/latency_bench_test.go - verify p99 <150ms

**Checkpoint**: At this point, User Stories 1 AND 2 should both work with pooling + fast libraries

---

## Phase 5: User Story 3 - Memory Safety and Leak Prevention (Priority: P1)

**Goal**: Enforce clear ownership semantics preventing memory leaks, double-frees, use-after-free

**Independent Test**: Run with race detector for 24 hours, confirm no leaks, no double-Put(), no races

### Memory Safety Tests for US3

- [X] T058 [P] [US3] Unit test in tests/unit/pool_lifecycle_test.go - verify panic message for double-Put() includes stack trace
- [X] T059 [P] [US3] Unit test in tests/unit/pool_lifecycle_test.go - verify IsReturned() flag prevents double-Put()
- [X] T060 [US3] Integration test in tests/integration/memory_leak_test.go (24hr, build tag) - run with pprof, verify no leaks

### Graceful Shutdown

- [X] T061 [US3] Implement PoolManager.Shutdown() in internal/pool/manager.go with 5-second timeout
- [X] T062 [US3] Add WaitGroup.Wait() logic in PoolManager.Shutdown() to track in-flight objects
- [X] T063 [US3] Add logging for unreturned objects on shutdown timeout in PoolManager.Shutdown()
- [X] T064 [US3] Wire PoolManager.Shutdown() into cmd/gateway/main.go graceful shutdown handler

### Debug Build Enhancements

- [X] T065 [P] [US3] Add debug-build field poisoning in internal/pool/bounded.go Put() (set fields to sentinel values to detect misuse)
- [X] T066 [P] [US3] Add debug-build stack trace capture on Get() in internal/pool/bounded.go for leak investigation

### Race Detection Validation

- [X] T067 [US3] Run all tests with -race flag in CI - update .github/workflows/ci.yml or Makefile
- [X] T068 [US3] Add -race integration test job in CI that runs for 5 minutes under load

**Checkpoint**: All user stories (US1, US2, US3) should now be independently functional with safety guarantees

---

## Phase 6: User Story 4 - Enforced Library Standards (Priority: P2)

**Goal**: CI prevents accidental use of banned libraries (encoding/json, gorilla/websocket)

**Independent Test**: Attempt to add banned imports, verify CI fails with clear error messages

### CI Enforcement for US4

- [X] T069 [P] [US4] Create .golangci.yml with depguard linter configuration
- [X] T070 [P] [US4] Add depguard rule to ban encoding/json with message "Use github.com/goccy/go-json instead"
- [X] T071 [P] [US4] Add depguard rule to ban gorilla/websocket with message "Use github.com/coder/websocket instead"
- [X] T072 [US4] Update .github/workflows/ci.yml to run golangci-lint with depguard enabled
- [X] T073 [US4] Update Makefile to add `make lint` target running golangci-lint

### Negative Tests for US4

- [X] T074 [P] [US4] Integration test in tests/integration/ci_enforcement_test.go - attempt encoding/json import, verify failure
- [X] T075 [P] [US4] Integration test in tests/integration/ci_enforcement_test.go - attempt gorilla/websocket import, verify failure
- [X] T076 [US4] Integration test in tests/integration/ci_enforcement_test.go - verify approved libraries pass checks

**Checkpoint**: CI enforcement prevents library regressions

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

### Documentation

- [X] T077 [P] Update README.md to reflect coder/websocket and goccy/go-json usage
- [X] T078 [P] Add MIGRATION.md documenting library changes and breaking changes
- [X] T079 [P] Update docs/architecture.md with pooling architecture diagrams
- [X] T080 [P] Add inline documentation for pool lifecycle in internal/pool/manager.go

### Migration Cleanup

- [X] T081 [P] Verify all gorilla/websocket imports removed
  - Run: `grep -rn 'gorilla/websocket' --include='*.go' internal/ cmd/ lib/ || echo "✓ PASS: No gorilla/websocket imports found"`
  - If found: Remove remaining imports and replace with coder/websocket
  - Expected: No matches (PASS message)

- [X] T082 [P] Verify all encoding/json imports removed
  - Run: `grep -rn '"encoding/json"' --include='*.go' internal/ cmd/ lib/ || echo "✓ PASS: No encoding/json imports found"`
  - Exclude: vendor/, tests/ (test files may mock encoding/json for equivalence tests)
  - If found: Replace with `json "github.com/goccy/go-json"`
  - Expected: No matches in production code (PASS message)

- [X] T083 [P] Verify no backward compatibility shims or feature flags exist
  - Run: `grep -rn 'compat\|legacy\|deprecated.*pool\|USE_OLD' --include='*.go' internal/ || echo "✓ PASS: No compat code found"`
  - Expected: No matches (none expected per CQ-08/GOV-04)

- [X] T084 Remove unused gorilla/websocket from go.mod if no transitive dependencies
  - Run: `go mod tidy` then verify `go.mod` has no gorilla/websocket entry
  - Note: May remain if transitive dependency; verify not directly imported

### Performance Validation

- [X] T085 [P] Capture performance baseline metrics (run benchmarks before pooling, save to baseline.txt)
- [X] T086 [P] Run post-upgrade benchmarks (save to upgraded.txt)
- [X] T087 Compare baseline vs upgraded using benchstat tool, verify targets met:
  - 40% allocation reduction
  - 30% faster JSON parsing
  - 20% reduced WS overhead
  - p99 latency <150ms
  - p99 GC pause <10ms

### Code Quality

- [X] T088 [P] Run golangci-lint locally and fix any issues
- [X] T089 [P] Run go vet ./... and address findings
- [X] T090 Ensure test coverage ≥70% (run go test -cover ./...)

### Final Validation

- [X] T091 Run quickstart.md validation steps to ensure guide is accurate
- [X] T092 Execute 24-hour soak test with varying load patterns (50-150 symbols)
- [X] T093 Verify all success criteria met (SC-001 through SC-009 from spec.md)

### Additional Verification & Pooling Tasks

#### Clone Ownership Verification

- [X] T094 [US3] Verify no Put() calls on heap clones in consumer code
  - Run: `grep -rn "\.Put(" internal/consumer/ | grep -E "(CanonicalEvent|MergedEvent|ExecReport)" || echo "✓ PASS: No Put() on clones found"`
  - Expected: No matches (empty output or PASS message)
  - If matches found: Review and remove incorrect Put() calls on clones
  - Rationale: Consumers own heap clones until GC; calling Put() would cause panic or pool corruption

#### Complete OrderRequest/ExecReport Pooling

- [X] T095 [US1] Update order request creation to Get() pooled OrderRequest with 100ms timeout
  - Location: Order creation code (internal/dispatcher/ or internal/conductor/)
  - Pattern: Get() → populate → send → defer Put()
  - Expected: OrderRequest lifecycle matches other pooled types (WsFrame, CanonicalEvent, etc.)

- [X] T096 [US1] Update execution report handling to Get() pooled ExecReport with 100ms timeout
  - Location: Provider response handler (internal/adapters/binance/)
  - Pattern: Get() → parse response → route → defer Put()
  - Expected: ExecReport lifecycle matches other pooled types

- [X] T097 [P] [US1] Unit test for OrderRequest pooling in tests/unit/pool_lifecycle_test.go
  - Verify: Get/Put balanced, no leaks, timeout handling
  - Pattern: Same as tests T016-T021 for other pooled types

- [X] T098 [P] [US1] Unit test for ExecReport pooling in tests/unit/pool_lifecycle_test.go
  - Verify: Get/Put balanced, no leaks, timeout handling
  - Pattern: Same as tests T016-T021 for other pooled types

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3-6)**: All depend on Foundational phase completion
  - US1, US2, US3 can proceed in parallel (if staffed) - all P1 priority
  - US4 depends on US2 completion (needs library migration done first)
- **Polish (Phase 7)**: Depends on all user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 2 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 3 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 4 (P2)**: Depends on US2 (library migration must be done to enforce standards)

### Within Each User Story

- Tests (unit/integration) MUST be written and FAIL before implementation
- Pool infrastructure (Foundational) before pool usage
- Library replacements can happen independently
- Core implementation before integration
- Story complete before moving to next priority

### Parallel Opportunities

- **Setup**: All T001-T004 can run in parallel
- **Foundational**: 
  - T005-T008 (pool infrastructure) can run in parallel
  - T009-T014 (Reset() methods) can run in parallel
  - T015 runs after T009-T014 complete
- **US1 Tests**: T016-T023 can all run in parallel (different test cases)
- **US2 Tests**: T041-T044 can all run in parallel
- **US2 WebSocket Migration**: T045-T049 are in same file (sequential)
- **US2 JSON Migration**: T050-T053 can run in parallel (different files)
- **US3 Tests**: T058-T060 can run in parallel
- **US4 CI Rules**: T069-T071 can run in parallel
- **US4 Tests**: T074-T076 can run in parallel
- **Polish Docs**: T077-T080 can run in parallel

---

## Parallel Example: User Story 1

```bash
# Launch all tests for User Story 1 together:
Task: "Unit test for WsFrame.Reset() - verify all fields zeroed"
Task: "Unit test for ProviderRaw.Reset() - verify all fields zeroed"
Task: "Unit test for CanonicalEvent.Reset() - verify all fields zeroed"
Task: "Unit test for MergedEvent.Reset() - verify all fields zeroed"
Task: "Unit test for OrderRequest.Reset() - verify all fields zeroed"
Task: "Unit test for ExecReport.Reset() - verify all fields zeroed"
Task: "Unit test for double-Put() panic - verify panic with stack trace"
Task: "Unit test for pool exhaustion timeout - verify context.DeadlineExceeded"

# After tests fail, launch consumer updates in parallel:
Task: "Update consumer handlers to process heap clones without Put()"
Task: "Add documentation comments explaining clone ownership"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (T001-T004)
2. Complete Phase 2: Foundational (T005-T015) - CRITICAL blocking phase
3. Complete Phase 3: User Story 1 (T016-T040)
4. **STOP and VALIDATE**: Test US1 independently - verify memory footprint reduction
5. Deploy/demo if ready

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test independently → Deploy/Demo (MVP: Memory Efficiency!)
3. Add User Story 2 → Test independently → Deploy/Demo (MVP + Latency!)
4. Add User Story 3 → Test independently → Deploy/Demo (MVP + Safety!)
5. Add User Story 4 → Test independently → Deploy/Demo (Full Feature!)
6. Each story adds value without breaking previous stories

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Foundational together (MUST be done first)
2. Once Foundational is done:
   - Developer A: User Story 1 (Memory Pooling)
   - Developer B: User Story 2 (Library Migration)
   - Developer C: User Story 3 (Safety & Shutdown)
3. Developer D: User Story 4 after US2 completes
4. Stories complete and integrate independently

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label (US1, US2, US3, US4) maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Verify tests fail before implementing (TDD approach)
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Avoid: vague tasks, same file conflicts, cross-story dependencies that break independence
- Pool infrastructure (Foundational Phase 2) is CRITICAL - all pooling depends on it
- Library migration (US2) can happen independently of pooling (US1)
- CI enforcement (US4) requires library migration (US2) to be complete first
- Debug build field poisoning helps catch pool misuse during development
- 24-hour leak test (US3) should run in background/overnight
- Benchmarking (Phase 7) validates all performance targets from spec
- Breaking changes are acceptable per CQ-08/GOV-04 - no shims or backward compat needed
- Tasks T094-T098 added per analysis recommendations to close coverage gaps (clone verification, OrderRequest/ExecReport pooling)

