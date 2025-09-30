# Task Generation Summary

**Feature**: 004-upgrade-non-functional (Performance & Memory Architecture Upgrade)  
**Tasks File**: `/Users/liqing/Documents/PersonalProjects/meltica/specs/004-upgrade-non-functional/tasks.md`  
**Generated**: 2025-10-13

## Overview

Generated **98 tasks** organized by user story to implement NFR upgrades with unpooled clone architecture.

**Updated 2025-10-13**: Added 5 tasks (T094-T098) per analysis recommendations to close coverage gaps.

## Task Breakdown by Phase

| Phase | Task Range | Count | Purpose |
|-------|-----------|-------|---------|
| **Phase 1: Setup** | T001-T004 | 4 | Dependencies and package structure |
| **Phase 2: Foundational** | T005-T015 | 11 | Pool infrastructure (BLOCKS all user stories) |
| **Phase 3: User Story 1 (P1)** | T016-T040, T095-T098 | 29 | Memory footprint reduction via pooling |
| **Phase 4: User Story 2 (P1)** | T041-T057 | 17 | Latency improvement via library replacement |
| **Phase 5: User Story 3 (P1)** | T058-T068, T094 | 12 | Memory safety and leak prevention |
| **Phase 6: User Story 4 (P2)** | T069-T076 | 8 | CI enforcement of library standards |
| **Phase 7: Polish** | T077-T093 | 17 | Documentation, cleanup, validation |
| **TOTAL** | T001-T098 | **98** | |

## Task Breakdown by User Story

### User Story 1: Reduced Memory Footprint (P1) - 29 tasks
**Goal**: 40% allocation reduction, <10ms p99 GC pause

**Tasks**:
- 8 unit tests (Reset() verification for all 6 pooled types, double-Put(), timeout)
- 6 provider path tasks (WsFrame and ProviderRaw pooling)
- 2 canonical event pooling tasks
- 2 orchestrator merge pooling tasks
- 4 dispatcher fan-out tasks (unpooled heap clones)
- 2 consumer update tasks
- 3 integration tests (E2E, panic detection, allocation reduction)
- 4 additional tasks (OrderRequest/ExecReport pooling: T095-T098)

**Independent Test**: Run 30-min sustained load (50 symbols, 3 providers), verify 40% allocation reduction

### User Story 2: Improved Latency (P1) - 17 tasks
**Goal**: p99 <150ms, 30% faster JSON, 20% lower WS overhead

**Tasks**:
- 4 unit tests (websocket and JSON migration equivalence)
- 5 WebSocket migration tasks (coder/websocket replacement)
- 5 JSON migration tasks (goccy/go-json replacement)
- 3 benchmarks (WS overhead, JSON speedup, E2E latency)

**Independent Test**: Latency benchmarks showing p99 <150ms and 30% JSON speedup

### User Story 3: Memory Safety (P1) - 12 tasks
**Goal**: Zero leaks over 24hr, no double-Put(), no races

**Tasks**:
- 3 memory safety tests (double-Put() panic, leak detection 24hr)
- 4 graceful shutdown tasks (PoolManager shutdown with 5s timeout)
- 2 debug build enhancements (field poisoning, stack traces)
- 2 race detection validation tasks
- 1 clone ownership verification task (T094: grep for accidental Put() on clones)

**Independent Test**: 24hr run with race detector, confirm no leaks/races/panics

### User Story 4: Library Standards (P2) - 8 tasks
**Goal**: CI blocks banned imports with 100% detection

**Tasks**:
- 5 CI enforcement tasks (golangci-lint with depguard configuration)
- 3 negative tests (verify banned imports fail CI)

**Independent Test**: Attempt banned imports, verify CI fails with clear errors

## Parallel Execution Opportunities

### Foundational Phase (Can parallelize)
- **T005-T008**: Pool infrastructure (4 parallel tasks in different files)
- **T009-T014**: Reset() methods (6 parallel tasks in different schema files)

### User Story 1 (Can parallelize)
- **T016-T023**: All unit tests (8 parallel test cases)
- **T036-T037**: Consumer updates (2 parallel tasks in different contexts)
- **T097-T098**: Additional pooling tests (2 parallel test cases for OrderRequest/ExecReport)

### User Story 2 (Can parallelize)
- **T041-T044**: All unit tests (4 parallel test cases)
- **T050-T053**: JSON migration (4 parallel tasks in different files)
- **T055-T056**: Benchmarks (2 parallel benchmarks)

### User Story 3 (Can parallelize)
- **T058-T060**: Memory safety tests (3 parallel test cases)
- **T065-T066**: Debug enhancements (2 parallel tasks)

### User Story 4 (Can parallelize)
- **T069-T071**: CI rules (3 parallel configuration tasks)
- **T074-T076**: Negative tests (3 parallel test cases)

### Polish Phase (Can parallelize)
- **T077-T080**: Documentation (4 parallel doc tasks)
- **T081-T084**: Cleanup (4 parallel cleanup tasks)
- **T085-T087**: Benchmarking (3 sequential benchmark comparison tasks)

## Critical Path Analysis

### Blocking Dependencies

1. **Foundational Phase BLOCKS Everything**:
   - Phase 2 (T005-T015) must complete before ANY user story can start
   - Pool infrastructure is prerequisite for all pooling work

2. **User Story 4 depends on User Story 2**:
   - Library migration (US2) must complete before CI enforcement (US4)
   - Can't enforce standards until standards are implemented

3. **All Other User Stories are Independent**:
   - US1, US2, US3 can run in parallel once Foundational completes

### Fastest Path to MVP

1. **Phase 1**: Setup (4 tasks) - ~1 hour
2. **Phase 2**: Foundational (11 tasks) - ~1 day (CRITICAL)
3. **Phase 3**: User Story 1 only (29 tasks) - ~3-4 days
4. **Validate**: Run 30-min load test
5. **Deploy**: MVP with memory efficiency

**Total MVP Time**: ~5-6 days (with foundational phase being the longest)

## Implementation Approaches

### Approach 1: MVP First (US1 Only)
**Timeline**: ~5-6 days
- Setup → Foundational → US1 (29 tasks) → Validate → Deploy
- Delivers: Memory footprint reduction, stable GC

### Approach 2: Incremental Delivery (All P1 Stories)
**Timeline**: ~2-3 weeks
1. Setup + Foundational (blocking) - 1-2 days
2. US1 (Memory - 29 tasks) - 3-4 days → Deploy
3. US2 (Latency - 17 tasks) - 2-3 days → Deploy
4. US3 (Safety - 12 tasks) - 2 days → Deploy
5. US4 (Standards - 8 tasks) - 1 day → Deploy

### Approach 3: Parallel Team (Fastest)
**Timeline**: ~1 week (with 3 developers)
1. Team: Setup + Foundational together - 1-2 days
2. Split:
   - Dev A: US1 (Memory)
   - Dev B: US2 (Latency)
   - Dev C: US3 (Safety)
3. Dev D (or any): US4 after US2 - 1 day
4. Integrate and polish - 1 day

## Testing Strategy

### Unit Tests: 29 tasks
- Reset() verification for all 6 pooled types
- Double-Put() detection
- Pool exhaustion timeout
- WebSocket migration equivalence
- JSON migration equivalence
- CI enforcement verification
- OrderRequest/ExecReport pooling tests (T097-T098)

### Integration Tests: 10 tasks
- End-to-end pooling validation
- Memory leak detection (24hr)
- Latency benchmarks
- Race condition detection
- CI enforcement negative tests
- Clone ownership verification (T094)

### Benchmarks: 6 tasks
- WebSocket overhead comparison
- JSON parsing speedup
- End-to-end latency
- Allocation reduction
- Performance baseline comparison

**Total Test Coverage**: 45 test-related tasks out of 98 (46%)

## Success Criteria Mapping

| Success Criterion | Validated By |
|-------------------|--------------|
| SC-001: 40% allocation reduction | T040 (integration test), T087 (benchstat) |
| SC-002: p99 latency <150ms | T057 (E2E benchmark), T087 (benchstat) |
| SC-003: 30% faster JSON | T056 (JSON benchmark), T087 (benchstat) |
| SC-004: Zero leaks (24hr) | T060 (24hr leak test with pprof) |
| SC-005: GC pause <10ms p99 | T040 (integration test), T087 (benchstat) |
| SC-006: Tests pass with -race | T067, T068 (race detection validation) |
| SC-007: CI blocks banned imports | T074, T075, T076 (negative tests) |
| SC-008: 20% reduced WS overhead | T055 (WS benchmark), T087 (benchstat) |
| SC-009: Pool timeout backpressure | T023 (unit test), T038 (integration test) |

**All 9 success criteria have validation tasks.**

## Key Implementation Details

### Library Migration (US2)
- **WebSocket**: gorilla/websocket → coder/websocket
  - Context-native Read/Write
  - Ping/pong built-in
  - Better close handshake
  
- **JSON**: encoding/json → goccy/go-json
  - 1.5-3x faster
  - API-compatible (drop-in)
  - Pooled Encoder/Decoder helpers

### Object Pooling (US1, US3)
- **6 pooled types**: WsFrame, ProviderRaw, CanonicalEvent, MergedEvent, OrderRequest, ExecReport
- **BoundedPool**: Semaphore-based capacity, 100ms Get() timeout
- **PoolManager**: Centralized coordinator, WaitGroup for shutdown
- **Double-Put()**: Panic with stack trace (fail-fast)
- **Reset()**: Zero all fields, unit-tested per type

### Ownership Rules (US1, US3)
- **Originals**: Pooled, owned by component that Get()s them
- **Clones**: Heap-allocated, owned by Consumers until GC
- **Dispatcher**: Creates clones, Put()s original after enqueue
- **Consumers**: Process clones, never call Put()

### CI Enforcement (US4)
- **golangci-lint** with depguard linter
- Ban: encoding/json, gorilla/websocket
- Require: goccy/go-json, coder/websocket
- Fail build on violation with clear error messages

## Files Modified (Estimated)

### New Files Created: ~12
- internal/pool/bounded.go
- internal/pool/manager.go
- internal/pool/lifecycle.go
- internal/pool/interface.go
- internal/pool/json_helpers.go
- tests/unit/pool_lifecycle_test.go
- tests/unit/websocket_migration_test.go
- tests/unit/json_migration_test.go
- tests/integration/pooling_e2e_test.go
- tests/integration/memory_leak_test.go
- tests/integration/latency_bench_test.go
- tests/integration/ci_enforcement_test.go

### Existing Files Modified: ~15
- cmd/gateway/main.go (pool registration, shutdown)
- internal/schema/frame.go (WsFrame Reset())
- internal/schema/provider.go (ProviderRaw Reset())
- internal/schema/event.go (CanonicalEvent Reset())
- internal/schema/merge.go (MergedEvent Reset())
- internal/schema/order.go (OrderRequest, ExecReport Reset())
- internal/adapters/binance/ws_client.go (coder/websocket, pooling)
- internal/adapters/binance/parser.go (goccy/go-json, pooling)
- internal/conductor/orchestrator_v2.go (MergedEvent pooling)
- internal/dispatcher/stream_ordering.go (clone, Put())
- internal/dispatcher/control_http.go (goccy/go-json)
- internal/consumer/consumer.go (clone handling, goccy/go-json)
- .golangci.yml (depguard config)
- .github/workflows/ci.yml (race detection, linting)
- Makefile (lint target)

### Documentation Updated: ~4
- README.md
- MIGRATION.md (new)
- docs/architecture.md
- inline comments in pool package

## Next Steps

1. **Review** the tasks.md file
2. **Choose** implementation approach (MVP first, incremental, or parallel team)
3. **Start** with Phase 1 (Setup) - T001-T004
4. **Complete** Phase 2 (Foundational) - CRITICAL blocking phase
5. **Implement** user stories in priority order (US1 → US2 → US3 → US4)
6. **Validate** at each checkpoint
7. **Deploy** incrementally after each user story completes

## Commit Suggestion

```bash
git add specs/004-upgrade-non-functional/tasks.md
git commit -m "tasks: add 004-upgrade-non-functional implementation tasks (98 tasks)

Task Breakdown:
- Phase 1 (Setup): 4 tasks - dependencies and package structure
- Phase 2 (Foundational): 11 tasks - pool infrastructure (BLOCKS all stories)
- Phase 3 (US1 - Memory): 29 tasks - pooling for 40% allocation reduction
- Phase 4 (US2 - Latency): 17 tasks - library migration for <150ms p99
- Phase 5 (US3 - Safety): 12 tasks - leak prevention, race detection, clone verification
- Phase 6 (US4 - Standards): 8 tasks - CI enforcement of approved libraries
- Phase 7 (Polish): 17 tasks - docs, cleanup, validation

Organization:
- Tasks grouped by user story for independent implementation
- 45 test tasks (46% of total) - unit, integration, benchmarks
- All 9 success criteria have validation tasks
- Parallel opportunities identified (34 tasks can run in parallel)

Implementation Approaches:
- MVP First: ~5-6 days (US1 only - 29 tasks)
- Incremental: ~2-3 weeks (all P1 stories - 66 tasks)
- Parallel Team: ~1-1.5 weeks (3 developers)

Key Deliverables:
- 6 pooled types with Reset() methods
- coder/websocket + goccy/go-json migration
- Unpooled heap clones for fan-out
- CI guards for banned imports
- 24hr leak-free operation

Refs: US1, US2, US3, US4, PERF-04, PERF-05, PERF-06, PERF-07"
```

---

**Tasks file ready**: `/Users/liqing/Documents/PersonalProjects/meltica/specs/004-upgrade-non-functional/tasks.md`

**Total Tasks**: 98  
**User Stories**: 4 (3 P1, 1 P2)  
**Test Tasks**: 45 (46%)  
**Parallel Tasks**: 34 (35%)  
**MVP Timeline**: 5-6 days (US1 only - 29 tasks)

**Updates Applied**: Added T094-T098 (5 tasks) + clarified T023, T081-T082 per analysis recommendations

