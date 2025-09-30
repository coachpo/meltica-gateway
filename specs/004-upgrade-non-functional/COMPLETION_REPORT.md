# Implementation Plan Completion Report

**Feature**: 004-upgrade-non-functional (Performance & Memory Architecture Upgrade)  
**Branch**: `004-upgrade-non-functional`  
**Date**: 2025-10-13  
**Status**: ✅ Planning Complete

## Summary

Implementation plan successfully generated for performance and memory architecture upgrade featuring:
- Library replacements: coder/websocket (replaces gorilla/websocket), goccy/go-json (replaces encoding/json)
- Object pooling: sync.Pool for 6 hot-path struct types with Reset() methods
- Ownership rules: Dispatcher creates heap clones for fan-out; consumers own clones until GC
- CI enforcement: Ban encoding/json and gorilla/websocket imports

**Target Metrics**: 40% allocation reduction, <150ms p99 latency, 30% faster JSON parsing, zero memory leaks over 24hrs

## Artifacts Generated

### Phase 0: Outline & Research ✅

**Location**: `/Users/liqing/Documents/PersonalProjects/meltica/specs/004-upgrade-non-functional/research.md`

**Content**:
- coder/websocket migration patterns (context-native API, performance benchmarks)
- goccy/go-json migration patterns (drop-in replacement, 1.5-3x speedup)
- sync.Pool best practices (bounded wrapper, capacity discipline, Reset() patterns)
- Double-Put() detection mechanisms (ownership flags, panic on violation)
- Graceful shutdown with pooled objects (WaitGroup tracking, 5s timeout)
- CI enforcement patterns (golangci-lint with depguard)

**All clarifications resolved**: Pool exhaustion (block with timeout), double-Put() (panic), timeout duration (100ms), Reset() validation (unit tests), shutdown (5s timeout)

### Phase 1: Design & Contracts ✅

#### Data Model

**Location**: `/Users/liqing/Documents/PersonalProjects/meltica/specs/004-upgrade-non-functional/data-model.md`

**Content**:
- 6 pooled struct types: WsFrame, ProviderRaw, CanonicalEvent, MergedEvent, OrderRequest, ExecReport
- Pool management entities: BoundedPool, PoolManager, DispatcherClone
- Reset() method specifications for all pooled types
- Entity relationships and lifecycle state machines
- Validation rules and performance characteristics

#### API Contracts

**Location**: `/Users/liqing/Documents/PersonalProjects/meltica/specs/004-upgrade-non-functional/contracts/pool_lifecycle.yaml`

**Content**:
- PooledObject interface (Reset, SetReturned, IsReturned)
- BoundedPool API (Get with timeout, Put with double-Put detection)
- PoolManager API (RegisterPool, Get, Put, Shutdown)
- Error scenarios and handling strategies
- Performance requirements and testing requirements

#### Quickstart Guide

**Location**: `/Users/liqing/Documents/PersonalProjects/meltica/specs/004-upgrade-non-functional/quickstart.md`

**Content**:
- Step-by-step migration guide (10 steps)
- WebSocket replacement examples (gorilla → coder)
- JSON replacement examples (encoding/json → goccy/go-json)
- Pool manager initialization
- Get/Put usage patterns
- Dispatcher fan-out with heap clones
- Consumer handler patterns
- Graceful shutdown implementation
- Verification checklist and troubleshooting

### Implementation Plan

**Location**: `/Users/liqing/Documents/PersonalProjects/meltica/specs/004-upgrade-non-functional/plan.md`

**Content**:
- Technical context (Go 1.25, dependencies, performance goals, constraints)
- Constitution check (all gates passed ✅)
- Project structure (monolithic Go app, internal/pool package, lib/pool utilities)
- Complexity tracking (no violations)

### Agent Context Update ✅

**Updated File**: `/Users/liqing/Documents/PersonalProjects/meltica/CLAUDE.md`

**Changes**:
- Added language: Go 1.25
- Added database info: No persistence (configuration state only)
- Updated with current feature technologies

## Constitution Compliance

### Final Constitution Check ✅ PASSED

All constitutional requirements satisfied:

- ✅ **LM-01**: Immutable component boundaries preserved (pooling localized per component)
- ✅ **LM-02**: Canonical events and versioned schemas maintained
- ✅ **LM-03**: Strong per-stream ordering unchanged
- ✅ **LM-04**: Backpressure policy enhanced (pool exhaustion = backpressure signal)
- ✅ **LM-05**: Windowed merge unchanged
- ✅ **LM-06**: Idempotent orders maintained
- ✅ **LM-07**: Snapshot+diff unchanged
- ✅ **LM-08**: Ops-only telemetry unchanged
- ✅ **LM-09**: Restart simplicity maintained (shutdown timeout added)
- ✅ **PERF-04**: Directly implements goccy/go-json requirement
- ✅ **PERF-05**: Directly implements coder/websocket requirement
- ✅ **PERF-06**: Directly implements sync.Pool requirement
- ✅ **PERF-07**: Directly implements Dispatcher fan-out ownership rules
- ✅ **CQ-08/GOV-04**: Breaking change accepted; no backward compat
- ✅ **ARCH-01/02**: /lib boundaries honored
- ✅ **TS-01, TS-02, TS-03, TS-05**: Testing requirements included

**No violations. No justifications needed.**

## Success Criteria Alignment

| Criterion | Target | Plan Coverage |
|-----------|--------|---------------|
| SC-001 | 40% allocation reduction @ 1000 events/sec | ✅ Pool capacity calculations in data-model.md |
| SC-002 | p99 latency <150ms end-to-end | ✅ 100ms pool timeout aligns with latency SLA |
| SC-003 | 30% faster JSON parsing | ✅ goccy/go-json benchmarks in research.md |
| SC-004 | Zero memory leaks (24hr) | ✅ Shutdown timeout, WaitGroup tracking |
| SC-005 | GC pause <10ms p99 | ✅ Pool reduces allocations → lower GC pressure |
| SC-006 | Tests pass with -race | ✅ CI enforcement in plan.md |
| SC-007 | CI blocks banned imports | ✅ depguard config in research.md, quickstart.md |
| SC-008 | 20% reduced WS overhead | ✅ coder/websocket benchmarks in research.md |
| SC-009 | Pool timeout backpressure | ✅ 100ms timeout handling in data-model.md, quickstart.md |

**All success criteria addressed in plan.**

## Directory Structure Created

```
specs/004-upgrade-non-functional/
├── spec.md                     # Feature specification (exists)
├── plan.md                     # Implementation plan ✅ NEW
├── research.md                 # Phase 0 research ✅ NEW
├── data-model.md               # Phase 1 data model ✅ NEW
├── quickstart.md               # Phase 1 quickstart ✅ NEW
├── contracts/                  # Phase 1 contracts ✅ NEW
│   └── pool_lifecycle.yaml     # Pool API contract ✅ NEW
├── checklists/
│   └── requirements.md         # Spec quality checklist (exists)
└── COMPLETION_REPORT.md        # This file ✅ NEW
```

## Next Steps

### Immediate Actions

1. **Review Plan**: Review generated artifacts (plan.md, research.md, data-model.md, contracts/, quickstart.md)
2. **Validate Design**: Ensure pool lifecycle and ownership rules align with team understanding
3. **Approve Approach**: Confirm library replacements (coder/websocket, goccy/go-json) are acceptable

### Implementation Phase

Run `/speckit.tasks` to generate task breakdown:

```bash
/speckit.tasks
```

This will create `tasks.md` with:
- Setup tasks (Go dependency installation, CI configuration)
- Foundational tasks (pool infrastructure, Reset() methods)
- Library replacement tasks (WebSocket migration, JSON migration)
- Pooling integration tasks (WS client → Parser → Orchestrator → Dispatcher)
- Testing tasks (unit tests, integration tests, benchmarks)
- Deployment tasks (rollout, monitoring)

### Validation Commands

Before starting implementation:

```bash
# Validate CI can detect banned imports
golangci-lint run --enable-only=depguard

# Run existing tests with -race
go test ./... -race -count=1

# Capture performance baseline
go test -bench=. -benchmem > baseline.txt
```

## Files Ready for Commit

All planning artifacts are complete and ready to commit:

```bash
git add specs/004-upgrade-non-functional/plan.md
git add specs/004-upgrade-non-functional/research.md
git add specs/004-upgrade-non-functional/data-model.md
git add specs/004-upgrade-non-functional/quickstart.md
git add specs/004-upgrade-non-functional/contracts/
git add CLAUDE.md  # Agent context updated
git commit -m "plan: add 004-upgrade-non-functional implementation plan

Phase 0 (Research):
- coder/websocket migration patterns (context-native API, 10-25% CPU reduction)
- goccy/go-json migration patterns (1.5-3x speedup, drop-in replacement)
- sync.Pool with bounded capacity (100ms timeout, double-Put detection)
- Graceful shutdown with 5s timeout for in-flight objects
- CI enforcement via golangci-lint depguard

Phase 1 (Design):
- 6 pooled structs: WsFrame, ProviderRaw, CanonicalEvent, MergedEvent, OrderRequest, ExecReport
- Reset() methods with unit test validation
- BoundedPool wrapper (capacity discipline, timeout)
- PoolManager (centralized coordinator, shutdown tracking)
- Dispatcher fan-out: heap clones for subscribers, Put() original
- Consumer ownership: clones owned until GC, no Put()

Contracts:
- Pool lifecycle API (Get/Put/Reset)
- Error scenarios (exhaustion, double-Put, shutdown timeout)
- Performance requirements (allocation reduction, latency targets)

Quickstart:
- 10-step migration guide
- Library replacement examples
- Pool usage patterns
- Graceful shutdown implementation

Constitution: All gates passed
Success Criteria: All 9 criteria addressed

Refs: PERF-04, PERF-05, PERF-06, PERF-07"
```

---

## Summary

✅ **Planning Phase Complete**

All deliverables generated:
- Implementation plan (plan.md)
- Research findings (research.md)
- Data model (data-model.md)
- API contracts (contracts/pool_lifecycle.yaml)
- Quickstart guide (quickstart.md)
- Agent context updated (CLAUDE.md)

**Ready for**: `/speckit.tasks` to generate task breakdown

**Branch**: `004-upgrade-non-functional` (current)  
**Feature Spec**: `/Users/liqing/Documents/PersonalProjects/meltica/specs/004-upgrade-non-functional/spec.md`  
**Implementation Plan**: `/Users/liqing/Documents/PersonalProjects/meltica/specs/004-upgrade-non-functional/plan.md`

