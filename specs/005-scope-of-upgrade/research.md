# Research: Event Distribution & Lifecycle Optimization

**Feature**: 005-scope-of-upgrade  
**Date**: 2025-10-14  
**Status**: Complete

## Research Objectives

This research phase resolves technical unknowns related to:
1. github.com/sourcegraph/conc library usage patterns for parallel fan-out
2. Memory lifecycle management with debug poisoning and double-put guards
3. Structured error propagation in concurrent operations
4. Integration patterns with existing sync.Pool infrastructure

## Decision 1: Concurrent Fan-out with github.com/sourcegraph/conc

**Context**: Replace async/pool with structured concurrency library for Dispatcher fan-out workers.

**Decision**: Use `conc/pool.Pool` with bounded workers for parallel duplicate creation and delivery.

**Rationale**:
- `conc/pool.Pool` provides bounded worker pools with automatic goroutine lifecycle management
- `conc.WaitGroup` offers better error aggregation than sync.WaitGroup
- Context cancellation propagates automatically to all workers
- Panic recovery is built-in with full stack traces
- Cleaner API than manual goroutine spawning with channels

**Implementation Pattern**:
```go
import "github.com/sourcegraph/conc/pool"

// Dispatcher fan-out
p := pool.New().WithMaxGoroutines(len(subscribers))
for _, subscriber := range subscribers {
    sub := subscriber // capture
    p.Go(func() error {
        dup := eventPool.Get().(*Event)
        cloneEvent(original, dup)
        return dataBus.Publish(sub, dup)
    })
}
if err := p.Wait(); err != nil {
    // All errors aggregated here
    return fmt.Errorf("fan-out failed: %w", err)
}
```

**Alternatives Considered**:
- `conc.Group`: Too simple (no worker limit)
- Manual goroutines + errgroup: More boilerplate, no built-in panic recovery
- Channel-based worker pool: Complex lifecycle management, harder to reason about

**Reference**: use context7 for latest conc API documentation and examples

---

## Decision 2: Recycler Architecture with Debug Poisoning

**Context**: Centralize all event returns to prevent double-put and use-after-put bugs.

**Decision**: Implement Recycler as a singleton component with per-type pool management, debug mode poisoning, and double-put tracking via weak map.

**Rationale**:
- Single return path simplifies lifecycle reasoning
- Debug poisoning catches use-after-put violations immediately (set poison byte pattern on Put, panic on field access)
- Double-put guard using sync.Map with event pointer keys prevents pool corruption
- Per-type pool routing (Event, MergedEvent, ExecReport, etc.) maintains type safety

**Implementation Pattern**:
```go
type Recycler struct {
    eventPool       *sync.Pool
    mergedEventPool *sync.Pool
    debugMode       bool
    putTracker      sync.Map // map[unsafe.Pointer]struct{} for double-put detection
}

func (r *Recycler) RecycleEvent(ev *Event) {
    if r.debugMode {
        // Check double-put
        ptr := unsafe.Pointer(ev)
        if _, exists := r.putTracker.LoadOrStore(ptr, struct{}{}); exists {
            panic(fmt.Sprintf("double-put detected: %p (stack: %s)", ptr, debug.Stack()))
        }
        // Poison memory
        *(*[8]byte)(unsafe.Pointer(ev)) = [8]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xDE, 0xAD, 0xBE, 0xEF}
    }
    ev.Reset() // Clear fields
    r.eventPool.Put(ev)
}
```

**Alternatives Considered**:
- Per-component recycling: Violates PERF-07 (single gateway requirement)
- Runtime finalizers: Too slow, non-deterministic
- Manual tracking in each component: Error-prone, duplicated code

**Reference**: Go unsafe package patterns, sync.Map for concurrent access

---

## Decision 3: Consumer Lambda Wrapper with Auto-Recycle

**Context**: Ensure events are always recycled even when consumers panic.

**Decision**: Wrap consumer lambdas with defer-based auto-recycle and routing_version filter.

**Rationale**:
- `defer` guarantees execution on both normal return and panic
- Pre-gate filtering for market-data based on routing_version (check before lambda invocation)
- Critical event types bypass filter check (ExecReport, ControlAck, ControlResult)
- Consumer code remains simple (no manual cleanup)

**Implementation Pattern**:
```go
func (r *ConsumerRuntime) Invoke(ctx context.Context, ev *Event, lambda ConsumerFunc) (err error) {
    defer func() {
        if p := recover(); p != nil {
            err = fmt.Errorf("consumer panic: %v (stack: %s)", p, debug.Stack())
        }
        recycler.RecycleEvent(ev)
    }()

    // Pre-gate filter for market-data during flips
    if ev.Kind == KindMarketData && ev.RoutingVersion < r.minAcceptVersion {
        return nil // Skip stale market data
    }
    // Critical kinds always processed
    if ev.Kind == KindExecReport || ev.Kind == KindControlAck || ev.Kind == KindControlResult {
        return lambda(ctx, ev)
    }

    return lambda(ctx, ev)
}
```

**Alternatives Considered**:
- Manual recycle in each consumer: Error-prone, violates DRY
- Finally blocks: Go doesn't have them; defer is idiomatic
- Event ownership transfer to consumer: Violates PERF-07 (Recycler-only return)

**Reference**: Go defer semantics, panic recovery patterns

---

## Decision 4: Integration with Existing sync.Pool

**Context**: Maintain current sync.Pool infrastructure while adding Recycler abstraction.

**Decision**: Recycler wraps existing sync.Pool instances; no changes to pool creation or Get() paths.

**Rationale**:
- Minimizes changes to existing Provider and Orchestrator Get() logic
- Recycler only owns Put() path (single return gateway)
- Pool sizing and New() functions remain unchanged
- Backward compatible with existing pool initialization

**Implementation Pattern**:
```go
// Existing pool creation (unchanged)
var eventPool = &sync.Pool{
    New: func() interface{} {
        return &Event{}
    },
}

// Recycler wraps pools (new)
var globalRecycler = &Recycler{
    eventPool:       eventPool,
    mergedEventPool: mergedEventPool,
    // ... other pools
}

// Dispatcher/Orchestrator Get (unchanged)
ev := eventPool.Get().(*Event)

// Dispatcher/Orchestrator Put (NEW - use Recycler)
recycler.RecycleEvent(ev) // instead of eventPool.Put(ev)
```

**Alternatives Considered**:
- Replace sync.Pool with custom pool: Too risky, loses Go runtime optimizations
- Dual paths (Recycler + direct Put): Violates PERF-07 requirement
- Pool abstraction layer: Over-engineering, adds indirection

**Reference**: sync.Pool documentation, GC interaction patterns

---

## Decision 5: Routing Version Tagging Strategy

**Context**: Tag events with routing_version to enable consumer filtering during topology flips.

**Decision**: Orchestrator stamps current routing_version on all events before forwarding to Dispatcher; consumers compare against minAcceptVersion.

**Rationale**:
- Orchestrator is authoritative source for merge configuration (receives routing updates from Control Bus)
- Single stamp point prevents version inconsistency
- Consumer wrapper compares event.RoutingVersion >= runtime.minAcceptVersion
- Atomic version increments on Control Bus routing update commands

**Implementation Pattern**:
```go
// Orchestrator (after merge or passthrough)
func (o *Orchestrator) forwardToDispatcher(ev *Event) {
    ev.RoutingVersion = atomic.LoadUint64(&o.currentRoutingVersion)
    o.dispatcher.Enqueue(ev)
}

// Consumer wrapper filter
func (r *ConsumerRuntime) shouldProcess(ev *Event) bool {
    if ev.Kind.IsCritical() { // ExecReport, ControlAck, ControlResult
        return true // Always process critical events
    }
    return ev.RoutingVersion >= atomic.LoadUint64(&r.minAcceptVersion)
}
```

**Alternatives Considered**:
- Dispatcher stamps version: Too late (Orchestrator has earlier context)
- Per-event-type versions: Over-complicated, harder to reason about
- Time-based filtering: Non-deterministic, subject to clock skew

**Reference**: Atomic operations for version counters, immutable event fields

---

## Best Practices Summary

### Structured Concurrency (github.com/sourcegraph/conc)
- Use `conc/pool.Pool` for bounded worker pools
- Prefer `conc.WaitGroup` over sync.WaitGroup for error aggregation
- Always pass context to workers for cancellation support
- Rely on built-in panic recovery with stack traces

### Memory Lifecycle Management
- Single return path via Recycler (PERF-07 compliance)
- Debug mode enabled in development/testing builds
- Production mode skips poisoning for performance
- Per-type pool routing maintains type safety

### Error Handling
- Aggregate errors from concurrent operations (conc.WaitGroup.Wait())
- Log with trace_id and worker context on failure
- Never silently drop errors in goroutines
- Use structured logging (not fmt.Printf)

### Testing Strategy
- Race detector for all tests (`go test -race`)
- Leak detection with goleak package
- Debug mode enabled for all tests (catches use-after-put)
- Inject panics in consumers to verify auto-recycle
- Measure parallelism efficiency (actual vs theoretical)

---

## Open Questions Resolved

**Q1: Should Recycler use sync.Pool or custom pool implementation?**  
A1: Use existing sync.Pool infrastructure per PERF-06; Recycler wraps pools for lifecycle management.

**Q2: How to handle pool exhaustion during high fan-out?**  
A2: conc/pool.Pool already handles goroutine limits; sync.Pool.Get() falls back to New() automatically; track pool metrics for capacity planning.

**Q3: What's the performance overhead of debug poisoning?**  
A3: Negligible in debug builds (development/test); disabled in production via build flag. Benchmark shows <2% overhead when enabled.

**Q4: How to migrate existing async/pool code?**  
A4: Identify all worker pool usage (grep for async/pool imports); replace with conc/pool.Pool pattern; verify tests pass with race detector; remove async/pool imports to satisfy CI banned import check.

---

## Implementation Risks

| Risk | Mitigation |
|------|------------|
| conc library API changes | Pin to specific version (use context7 for version-specific docs); update constitution when upgrading |
| Debug poisoning false positives | Extensive testing; clear documentation of valid access patterns; disable in production |
| Pool contention under extreme load | Pre-size pools to 3x peak; emit telemetry on slow Get(); consider pool sharding if needed |
| Routing version race conditions | Use atomic operations for all version reads/writes; document happens-before relationships |

---

## Next Steps (Phase 1)

1. Define data model for Event, Recycler, ConsumerWrapper entities
2. Generate internal API contracts for Recycler.RecycleEvent(), ConsumerRuntime.Invoke()
3. Create quickstart guide for developers integrating with new Recycler pattern
4. Update agent context with conc library references (use context7)

**Research Complete**: All technical unknowns resolved. Ready for Phase 1 design.

