# Tasks: Event Distribution & Lifecycle Optimization

**Input**: Design documents from `/specs/005-scope-of-upgrade/`
**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`
- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3, US4)
- Include exact file paths in descriptions

## Path Conventions
- **Core components**: `/home/qing/work/meltica/core/`
- **Tests**: `/home/qing/work/meltica/tests/`
- **Documentation**: `/home/qing/work/meltica/specs/005-scope-of-upgrade/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and dependency installation

- [X] T001 [P] Add `github.com/sourcegraph/conc@latest` dependency to go.mod
- [X] T002 [P] Add `github.com/uber-go/goleak@latest` dependency to go.mod for leak detection tests
- [X] T003 [P] Update CI configuration (.github/workflows/ci.yml) to add `async/pool` to banned imports list
- [X] T004 Create `/home/qing/work/meltica/core/recycler/` directory structure for new component

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core Recycler infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T005 [P] Create EventKind enum type in `/home/qing/work/meltica/core/events/kind.go` with IsCritical() method
- [X] T006 [P] Add RoutingVersion field to Event struct in `/home/qing/work/meltica/core/events/event.go`
- [X] T007 Create Recycler interface in `/home/qing/work/meltica/core/recycler/interface.go` with RecycleEvent(), RecycleMergedEvent(), RecycleMany() methods
- [X] T008 Implement RecyclerImpl struct in `/home/qing/work/meltica/core/recycler/recycler.go` with pool references and debugMode flag
- [X] T009 Implement RecycleEvent() method in `/home/qing/work/meltica/core/recycler/recycler.go` with Reset() and optional poisoning
- [X] T010 Implement RecycleMergedEvent() method in `/home/qing/work/meltica/core/recycler/recycler.go`
- [X] T011 Implement RecycleMany() bulk recycle method in `/home/qing/work/meltica/core/recycler/recycler.go`
- [X] T012 [P] Implement debug poisoning logic in `/home/qing/work/meltica/core/recycler/debug.go` (poison first 8 bytes with 0xDEADBEEFDEADBEEF)
- [X] T013 [P] Implement double-put guard using sync.Map in `/home/qing/work/meltica/core/recycler/debug.go`
- [X] T014 Add EnableDebugMode() and DisableDebugMode() methods to `/home/qing/work/meltica/core/recycler/recycler.go`
- [X] T015 Create global singleton instance in `/home/qing/work/meltica/core/recycler/global.go` initialized at startup
- [X] T016 [P] Add RecyclerMetrics struct in `/home/qing/work/meltica/core/recycler/metrics.go` with prometheus counters
- [X] T017 [P] Instrument RecycleEvent() with metrics in `/home/qing/work/meltica/core/recycler/recycler.go`
- [X] T018 Unit test: Test RecycleEvent() resets fields in `/home/qing/work/meltica/tests/recycler_test.go`
- [X] T019 Unit test: Test debug mode poisoning detects use-after-put in `/home/qing/work/meltica/tests/recycler_test.go`
- [X] T020 Unit test: Test double-put guard panics on duplicate recycle in `/home/qing/work/meltica/tests/recycler_test.go`
- [X] T021 Unit test: Test RecycleMany() bulk operation in `/home/qing/work/meltica/tests/recycler_test.go`

**Checkpoint**: Recycler foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 1 - Parallel Event Delivery to Multiple Consumers (Priority: P1) 🎯 MVP

**Goal**: Replace sequential fan-out with parallel delivery using pool-backed duplicates to achieve <15ms latency for 10 subscribers

**Independent Test**: Subscribe 3 strategies to BTC-USDT, publish price update, verify all receive event within 10ms window while memory usage remains bounded

### Implementation for User Story 1

- [X] T022 [P] [US1] Locate and audit existing Dispatcher fan-out code in `/home/qing/work/meltica/core/dispatcher/fanout.go` for async/pool usage
- [X] T023 [US1] Replace heap-allocated event clones with pool.Get() duplicates in `/home/qing/work/meltica/core/dispatcher/fanout.go`
- [X] T024 [US1] Implement parallel fan-out using conc/pool.Pool in `/home/qing/work/meltica/core/dispatcher/fanout.go` with bounded goroutines
- [X] T025 [US1] Add cloneEvent() helper function in `/home/qing/work/meltica/core/dispatcher/fanout.go` to copy Event fields into duplicate
- [X] T026 [US1] Ensure original event is recycled via Recycler.RecycleEvent() after all duplicates sent in `/home/qing/work/meltica/core/dispatcher/fanout.go`
- [X] T027 [US1] Optimize single-subscriber path to deliver original directly (no duplicate creation) in `/home/qing/work/meltica/core/dispatcher/fanout.go`
- [X] T028 [US1] Recycle original immediately when zero subscribers in `/home/qing/work/meltica/core/dispatcher/fanout.go`
- [X] T029 [P] [US1] Add fan-out timing metrics (min/max/p95 delivery time) in `/home/qing/work/meltica/core/dispatcher/metrics.go`
- [X] T030 [P] [US1] Add parallelism efficiency metric in `/home/qing/work/meltica/core/dispatcher/metrics.go`
- [X] T031 [US1] Unit test: Test parallel fan-out delivers to 10 subscribers within 15ms in `/home/qing/work/meltica/tests/dispatcher_test.go`
- [X] T031b [US1] Unit test: Run parallel fan-out tests with `-race` flag to verify no data races during concurrent duplicate creation in `/home/qing/work/meltica/tests/dispatcher_test.go`
- [X] T032 [US1] Unit test: Test memory pool utilization remains <80% under 1000 events/sec load in `/home/qing/work/meltica/tests/dispatcher_test.go`
- [X] T033 [US1] Unit test: Test original event recycled after fan-out in `/home/qing/work/meltica/tests/dispatcher_test.go`
- [X] T034 [US1] Unit test: Test single-subscriber optimization (no duplicate) in `/home/qing/work/meltica/tests/dispatcher_test.go`
- [X] T035 [US1] Benchmark: Measure parallelism efficiency (>90% for 5+ subscribers) in `/home/qing/work/meltica/tests/dispatcher_bench_test.go`

**Checkpoint**: At this point, User Story 1 should be fully functional and testable independently

---

## Phase 4: User Story 2 - Automatic Resource Cleanup on Consumer Completion (Priority: P1) 🎯 MVP

**Goal**: Implement consumer wrapper with defer-based auto-recycle and panic recovery to achieve zero memory leaks over 24 hours

**Independent Test**: Process 10,000 events through consumer that panics every 100th event, verify all events recycled with zero leaks detected (goleak)

### Implementation for User Story 2

- [X] T036 [P] [US2] Create ConsumerWrapper interface in `/home/qing/work/meltica/core/consumer/wrapper.go` with Invoke() method
- [X] T037 [P] [US2] Create ConsumerWrapperImpl struct in `/home/qing/work/meltica/core/consumer/wrapper.go` with recycler reference
- [X] T038 [US2] Implement Invoke() method with defer-based auto-recycle in `/home/qing/work/meltica/core/consumer/wrapper.go`
- [X] T039 [US2] Add panic recovery logic in Invoke() defer block in `/home/qing/work/meltica/core/consumer/wrapper.go`
- [X] T040 [US2] Ensure recycler.RecycleEvent() always executes in defer (both return and panic paths) in `/home/qing/work/meltica/core/consumer/wrapper.go`
- [X] T041 [P] [US2] Add ConsumerMetrics struct in `/home/qing/work/meltica/core/consumer/metrics.go` with panic counter
- [X] T042 [P] [US2] Instrument Invoke() with processing duration histogram in `/home/qing/work/meltica/core/consumer/wrapper.go`
- [X] T043 [US2] Update Orchestrator merge logic in `/home/qing/work/meltica/core/orchestrator/merge.go` to call recycler.RecycleMany(partials) immediately after merge
- [X] T044 [US2] Remove direct eventPool.Put() calls from Orchestrator in `/home/qing/work/meltica/core/orchestrator/merge.go`
- [X] T045 [US2] Update all existing consumers to use ConsumerWrapper.Invoke() in `/home/qing/work/meltica/core/consumer/registry.go`
- [X] T046 [US2] Unit test: Test consumer wrapper auto-recycles on normal return in `/home/qing/work/meltica/tests/consumer_test.go`
- [X] T047 [US2] Unit test: Test consumer wrapper auto-recycles on panic in `/home/qing/work/meltica/tests/consumer_test.go`
- [X] T048 [US2] Unit test: Test panic recovery with stack trace logging in `/home/qing/work/meltica/tests/consumer_test.go`
- [X] T049 [US2] Unit test: Test Orchestrator recycles partials after merge in `/home/qing/work/meltica/tests/orchestrator_test.go`
- [X] T050 [US2] Integration test: Process 10,000 events with 10% panic rate, verify zero leaks (goleak) in `/home/qing/work/meltica/tests/integration/leak_test.go`
- [X] T051 [US2] Soak test: Run 1 million events over simulated 24-hour period, verify no memory growth in `/home/qing/work/meltica/tests/integration/soak_test.go`

**Checkpoint**: At this point, User Stories 1 AND 2 should both work independently (MVP complete!)

---

## Phase 5: User Story 3 - Selective Market Data Filtering During Topology Changes (Priority: P2)

**Goal**: Enable consumers to ignore stale market-data based on routing_version while guaranteeing 100% delivery of critical events

**Independent Test**: Trigger routing flip while sending market data + ExecReports, verify consumer ignores market data but receives all ExecReports

### Implementation for User Story 3

- [X] T052 [P] [US3] Add minAcceptVersion field to ConsumerWrapperImpl in `/home/qing/work/meltica/core/consumer/wrapper.go` (atomic access)
- [X] T053 [P] [US3] Implement ShouldProcess() method in `/home/qing/work/meltica/core/consumer/wrapper.go` with routing version check
- [X] T054 [US3] Add critical event bypass logic to ShouldProcess() in `/home/qing/work/meltica/core/consumer/wrapper.go` (ExecReport, ControlAck, ControlResult always return true)
- [X] T055 [US3] Implement UpdateMinVersion() method in `/home/qing/work/meltica/core/consumer/wrapper.go` using atomic.StoreUint64
- [X] T056 [US3] Add pre-gate filter call to ShouldProcess() before lambda invocation in Invoke() method in `/home/qing/work/meltica/core/consumer/wrapper.go`
- [X] T057 [US3] Return early (skip lambda) if ShouldProcess() returns false in `/home/qing/work/meltica/core/consumer/wrapper.go`
- [X] T058 [US3] Ensure recycler still called in defer even when event filtered in `/home/qing/work/meltica/core/consumer/wrapper.go`
- [X] T059 [US3] Update Orchestrator to stamp RoutingVersion on all events before Dispatcher in `/home/qing/work/meltica/core/orchestrator/stamp.go`
- [X] T060 [US3] Use atomic.LoadUint64 for current routing version in Orchestrator in `/home/qing/work/meltica/core/orchestrator/stamp.go`
- [X] T061 [P] [US3] Add filtered events counter metric in `/home/qing/work/meltica/core/consumer/metrics.go`
- [X] T062 [US3] Unit test: Test ShouldProcess() filters stale market data in `/home/qing/work/meltica/tests/consumer_test.go`
- [X] T063 [US3] Unit test: Test ShouldProcess() always processes ExecReport regardless of version in `/home/qing/work/meltica/tests/consumer_test.go`
- [X] T064 [US3] Unit test: Test ShouldProcess() always processes ControlAck and ControlResult in `/home/qing/work/meltica/tests/consumer_test.go`
- [X] T065 [US3] Unit test: Test UpdateMinVersion() atomic operation in `/home/qing/work/meltica/tests/consumer_test.go`
- [X] T066 [US3] Integration test: Trigger routing flip, verify market data filtered and ExecReports delivered in `/home/qing/work/meltica/tests/integration/routing_flip_test.go`
- [X] T067 [US3] Integration test: Verify <5% market data delivery during flip window in `/home/qing/work/meltica/tests/integration/routing_flip_test.go`

**Checkpoint**: At this point, User Stories 1, 2, AND 3 should all work independently

---

## Phase 6: User Story 4 - Structured Error Handling in Concurrent Operations (Priority: P2)

**Goal**: Migrate async/pool to github.com/sourcegraph/conc for proper error aggregation, panic recovery, and goroutine lifecycle management

**Independent Test**: Configure consumer to fail, trigger delivery, verify error captured with trace_id/consumer context and no goroutine leaks

### Implementation for User Story 4

- [X] T068 [US4] Audit codebase for all async/pool imports using grep in `/home/qing/work/meltica/`
- [X] T069 [US4] Replace async/pool worker pool with conc/pool.Pool in Dispatcher fan-out (if not already done in T024) in `/home/qing/work/meltica/core/dispatcher/fanout.go`
- [X] T070 [P] [US4] Locate any other async/pool usage in Orchestrator and replace with conc.WaitGroup in `/home/qing/work/meltica/core/orchestrator/`
- [X] T071 [P] [US4] Locate any other async/pool usage in Consumer runtime and replace with conc.WaitGroup in `/home/qing/work/meltica/core/consumer/`
- [X] T072 [US4] Ensure all conc.Pool.Go() workers accept context for cancellation in `/home/qing/work/meltica/core/dispatcher/fanout.go`
- [X] T073 [US4] Verify conc.Pool.Wait() aggregates all errors from workers in `/home/qing/work/meltica/core/dispatcher/fanout.go`
- [X] T074 [US4] Add structured logging for aggregated errors with trace_id in `/home/qing/work/meltica/core/dispatcher/fanout.go`
- [X] T075 [US4] Remove all async/pool imports from the codebase (verify with banned import CI check)
- [X] T076 [US4] Unit test: Test one consumer error doesn't block other deliveries in `/home/qing/work/meltica/tests/dispatcher_test.go`
- [X] T077 [US4] Unit test: Test worker panic is recovered and logged in `/home/qing/work/meltica/tests/dispatcher_test.go`
- [X] T078 [US4] Unit test: Test multiple errors are aggregated and reported in `/home/qing/work/meltica/tests/dispatcher_test.go`
- [X] T079 [US4] Unit test: Test context cancellation propagates to all workers in `/home/qing/work/meltica/tests/dispatcher_test.go`
- [X] T080 [US4] Integration test: Verify zero goroutine leaks with goleak after concurrent operations in `/home/qing/work/meltica/tests/integration/leak_test.go`
- [X] T081 [US4] Integration test: Verify error logging includes trace_id and consumer context in `/home/qing/work/meltica/tests/integration/error_test.go`

**Checkpoint**: All user stories should now be independently functional with proper error handling

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] T082 [P] Add Dispatcher Parallel Fan-out diagram to spec.md (PlantUML already provided in spec)
- [ ] T083 [P] Add Canonical Event Lifecycle diagram to spec.md (PlantUML already provided in spec)
- [ ] T084 [P] Update architecture.md with Recycler component description in `/home/qing/work/meltica/docs/architecture.md`
- [ ] T085 [P] Create README note about "use context7" prompt rule in `/home/qing/work/meltica/README.md` (Development section)
- [ ] T086 [P] Document Recycler ownership rules in quickstart.md (already in quickstart, verify completeness)
- [ ] T087 [P] Document async/pool → conc migration guide in quickstart.md (already in quickstart, verify completeness)
- [X] T088 Code cleanup: Remove commented-out async/pool code across all modified files
- [X] T089 Code cleanup: Remove unused pool.Put() calls from Provider code (verify not directly called)
- [X] T090 [P] Run go fmt on all modified Go files
- [X] T091 [P] Run golangci-lint on all modified files and resolve issues
- [X] T092 Verify CI passes: build, race detector, ≥70% coverage, banned imports check
- [X] T093 Run quickstart.md validation by following all 7 steps manually
- [X] T094 Performance validation: Benchmark parallel fan-out with 10 subscribers (<15ms target)
- [X] T095 Performance validation: Run 24-hour soak test to verify zero memory growth

---

## Phase 8: Success Criteria Validation

**Purpose**: Explicit validation of all measurable success criteria from spec.md

- [X] T096 [P] Performance validation: Benchmark parallel fan-out with 10 subscribers, verify <15ms total latency (SC-001) in `/home/qing/work/meltica/tests/dispatcher_bench_test.go`
- [X] T097 [P] Performance validation: Measure memory pool utilization under 1000 events/sec load, verify <80% utilization (SC-003) in `/home/qing/work/meltica/tests/integration/pool_metrics_test.go`
- [X] T098 [P] Performance validation: Benchmark parallelism efficiency with 5+ subscribers, verify >90% efficiency (SC-009) in `/home/qing/work/meltica/tests/dispatcher_bench_test.go`
- [X] T099 Debug validation: Enable debug mode, inject use-after-put scenario, verify detection within 1 event (SC-007) in `/home/qing/work/meltica/tests/recycler_test.go`
- [X] T100 Debug validation: Enable debug mode, attempt double-put, verify 100% detection and panic (SC-008) in `/home/qing/work/meltica/tests/recycler_test.go`
- [X] T101 Integration validation: Run 24-hour soak test with 1M+ events, verify zero memory growth (SC-002) in `/home/qing/work/meltica/tests/integration/soak_test.go`
- [X] T102 Integration validation: Process 100K events with 10% consumer panic rate, verify zero goroutine leaks via goleak (SC-005) in `/home/qing/work/meltica/tests/integration/leak_test.go`
- [X] T103 Integration validation: Trigger routing flip, verify 100% critical event delivery and <5% market data delivery (SC-004, SC-010) in `/home/qing/work/meltica/tests/integration/routing_flip_test.go`
- [X] T104 Observability validation: Inject consumer errors, verify 100% error capture with trace_id context (SC-006) in `/home/qing/work/meltica/tests/integration/error_test.go`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3-6)**: All depend on Foundational phase completion
  - US1 (Phase 3) can start after Foundational
  - US2 (Phase 4) can start after Foundational (uses Recycler from Phase 2)
  - US3 (Phase 5) can start after Foundational (uses ConsumerWrapper from US2 Phase 4)
  - US4 (Phase 6) can start after Foundational (modifies Dispatcher from US1 Phase 3)
- **Polish (Phase 7)**: Depends on all desired user stories being complete
- **Success Criteria Validation (Phase 8)**: Depends on all user stories and polish being complete; validates spec.md measurable outcomes

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 2 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories (but creates ConsumerWrapper used by US3)
- **User Story 3 (P2)**: Soft dependency on US2 (extends ConsumerWrapper) - Can be developed in parallel but easier after US2
- **User Story 4 (P2)**: Soft dependency on US1 (migrates Dispatcher pools) - Can be merged with US1 implementation

### Within Each User Story

- Foundational Phase: All Recycler tasks (T005-T021) sequential within component
- US1: Dispatcher modifications sequential within fanout.go
- US2: ConsumerWrapper creation before Orchestrator integration
- US3: ConsumerWrapper extension after base implementation
- US4: Audit before migration, migration before cleanup
- Polish: All tasks can run in parallel except final validations

### Parallel Opportunities

**Setup Phase (Phase 1)**:
```bash
# Launch all setup tasks together:
Task T001: Add conc dependency
Task T002: Add goleak dependency  
Task T003: Update CI banned imports
Task T004: Create recycler directory
```

**Foundational Phase (Phase 2)**:
```bash
# Parallel within sub-components:
Parallel Group A (Event types):
  Task T005: Create EventKind enum
  Task T006: Add RoutingVersion field

Parallel Group B (Recycler interface + debug):
  Task T007-T011: Recycler interface and implementation
  Task T012-T013: Debug poisoning and double-put guard (separate file)
  Task T016-T017: Metrics (separate file)
```

**User Story 1 (Phase 3)**:
```bash
# Parallel between implementation and metrics:
Parallel Group:
  Task T022-T028: Dispatcher fan-out implementation (sequential within)
  Task T029-T030: Metrics instrumentation
```

**User Story 2 (Phase 4)**:
```bash
# Parallel between wrapper and orchestrator:
Parallel Group A:
  Task T036-T040: ConsumerWrapper implementation
  Task T041-T042: Metrics

Parallel Group B:
  Task T043-T044: Orchestrator recycling
```

**After Phase 2 Complete - User Stories Can Proceed in Parallel**:
```bash
# If team has capacity, all P1 and P2 stories can be worked simultaneously:
Developer A: User Story 1 (T022-T035)
Developer B: User Story 2 (T036-T051)
Developer C: User Story 3 (T052-T067) - starts after US2 tasks T036-T040 complete
Developer D: User Story 4 (T068-T081) - coordinates with Developer A on Dispatcher
```

---

## Parallel Example: User Story 1

```bash
# After Foundational phase complete, launch US1 implementation:
Sequential Group (Dispatcher core):
  Task T022: Audit existing code
  Task T023: Replace heap clones with pool duplicates
  Task T024: Implement parallel fan-out
  Task T025: Add cloneEvent() helper
  Task T026: Recycle original after fan-out
  Task T027: Optimize single-subscriber path
  Task T028: Recycle on zero subscribers

Parallel Group (Metrics - different file):
  Task T029: Add fan-out timing metrics
  Task T030: Add parallelism efficiency metric

Sequential Group (Tests):
  Task T031-T034: Unit tests
  Task T035: Benchmark
```

---

## Implementation Strategy

### MVP First (User Stories 1 + 2 Only)

1. Complete Phase 1: Setup (T001-T004)
2. Complete Phase 2: Foundational (T005-T021) - CRITICAL blocking phase
3. Complete Phase 3: User Story 1 (T022-T035, including T031b race detector) - Parallel delivery
4. Complete Phase 4: User Story 2 (T036-T051) - Auto-recycle
5. **STOP and VALIDATE**: Test US1 + US2 independently
6. Run Phase 8 MVP validation subset (T096, T097, T098, T101, T102)
7. Deploy/demo if ready (MVP with both P1 stories)

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready (T001-T021)
2. Add User Story 1 → Test independently → Deploy/Demo (Parallel delivery working)
3. Add User Story 2 → Test independently → Deploy/Demo (Zero leaks working)
4. **MVP COMPLETE** at this point (both P1 stories delivered)
5. Run MVP validation (T096-T098, T101-T102) → Verify success criteria
6. Add User Story 3 → Test independently → Deploy/Demo (Smart filtering working)
7. Add User Story 4 → Test independently → Deploy/Demo (Error handling complete)
8. Add Polish → Final validation (Phase 8 complete T096-T104) → Deploy/Demo (All stories + docs)

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Foundational together (T001-T021)
2. Once Foundational is done:
   - Developer A: User Story 1 (T022-T035) - 14 tasks
   - Developer B: User Story 2 (T036-T051) - 16 tasks
   - Developer C: User Story 3 (T052-T067) - 16 tasks (starts after T036-T040 from Dev B)
   - Developer D: User Story 4 (T068-T081) - 14 tasks (coordinates with Dev A)
3. All developers: Polish phase (T082-T095) in parallel
4. Stories complete and integrate independently

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Avoid: vague tasks, same file conflicts, cross-story dependencies that break independence
- Ensure Recycler is single return gateway per PERF-07
- Verify all async/pool usage eradicated per PERF-09
- Maintain existing component boundaries per LM-01 (no changes to Provider, Data Bus, Control Bus)
- Use github.com/sourcegraph/conc for all worker pools with proper error aggregation
- Critical events (ExecReport, ControlAck, ControlResult) must bypass filtering per PERF-08
- Enable debug mode in development/test builds; disable in production
- Run all tests with -race flag (explicit race detector test in T031b); verify ≥70% coverage
- Race detector must pass for parallel fan-out (T031b), consumer wrapper panic recovery (T047), and all integration tests
- Use context7 when looking up conc library documentation in Cursor/agents per GOV-06

## Task Summary

**Total Tasks**: 105

**By Phase**:
- Phase 1 (Setup): 4 tasks
- Phase 2 (Foundational): 17 tasks
- Phase 3 (US1 - P1): 15 tasks (includes T031b race detector test)
- Phase 4 (US2 - P1): 16 tasks
- Phase 5 (US3 - P2): 16 tasks
- Phase 6 (US4 - P2): 14 tasks
- Phase 7 (Polish): 14 tasks
- Phase 8 (Success Criteria Validation): 9 tasks

**By Priority**:
- P1 (MVP): 31 tasks (US1 + US2, includes T031b)
- P2 (Enhanced): 30 tasks (US3 + US4)
- Infrastructure: 21 tasks (Setup + Foundational)
- Polish: 14 tasks
- Validation: 9 tasks (Success Criteria)

**Parallel Opportunities**: 26 tasks marked [P] for parallel execution (includes 3 new parallel tasks in Phase 8)

**Suggested MVP Scope**: Phase 1-4 (T001-T051) = 52 tasks delivering both P1 user stories (includes T031b race detector)

