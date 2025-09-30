# Quickstart: Event Distribution & Lifecycle Optimization

**Feature**: 005-scope-of-upgrade  
**Audience**: Developers implementing parallel fan-out, Recycler integration, and consumer wrappers  
**Date**: 2025-10-14

## Overview

This guide walks through integrating the new event distribution architecture with:
- **Recycler**: Centralized return gateway for all events
- **Parallel Fan-out**: Pool-backed duplicates with `github.com/sourcegraph/conc`
- **Consumer Wrapper**: Auto-recycle with routing_version filtering
- **Debug Mode**: Poisoning and double-put detection

## Prerequisites

1. **Go 1.25** installed
2. **Dependencies**:
   ```bash
   go get github.com/sourcegraph/conc@latest
   ```
3. **Environment Variables**:
   ```bash
   export RECYCLER_DEBUG_MODE=true  # Enable debug features in dev/test
   ```

---

## Step 1: Initialize Recycler (Startup)

The Recycler is a singleton initialized once at application startup.

### Code Example

```go
package main

import (
    "sync"
    "github.com/coachpo/meltica/core/recycler"
)

func initPools() map[string]*sync.Pool {
    return map[string]*sync.Pool{
        "event": {
            New: func() interface{} { return &Event{} },
        },
        "merged": {
            New: func() interface{} { return &MergedEvent{} },
        },
        "execreport": {
            New: func() interface{} { return &ExecReport{} },
        },
    }
}

func main() {
    // Initialize Recycler with debug mode in non-production
    debugMode := os.Getenv("RECYCLER_DEBUG_MODE") == "true"
    pools := initPools()
    
    recycler.GlobalRecycler = recycler.NewRecycler(pools, debugMode)
    
    if debugMode {
        log.Println("Recycler debug mode enabled (poisoning + double-put detection)")
    }
    
    // Start components...
}
```

### Validation

```bash
# Run with debug mode
RECYCLER_DEBUG_MODE=true go run main.go

# Expected output:
# Recycler debug mode enabled (poisoning + double-put detection)
```

---

## Step 2: Orchestrator Integration (Merge + Recycle Partials)

Orchestrator must stamp `RoutingVersion` and recycle partials immediately after merge.

### Code Example

```go
package orchestrator

import (
    "context"
    "sync/atomic"
    "github.com/coachpo/meltica/core/recycler"
)

type Orchestrator struct {
    currentRoutingVersion uint64 // Atomic
    mergedEventPool       *sync.Pool
    recycler              recycler.Recycler
}

func (o *Orchestrator) ProcessEvent(ctx context.Context, ev *Event) error {
    // Stamp routing version before forwarding
    ev.RoutingVersion = atomic.LoadUint64(&o.currentRoutingVersion)
    
    // Check if merge needed
    if o.needsMerge(ev) {
        merged, partials := o.merge(ctx, ev)
        
        // Recycle partials immediately via Recycler
        o.recycler.RecycleMany(partials)
        
        // Forward merged event
        return o.forwardToDispatcher(ctx, merged)
    }
    
    // Passthrough (no merge)
    return o.forwardToDispatcher(ctx, ev)
}

func (o *Orchestrator) merge(ctx context.Context, ev *Event) (*MergedEvent, []*Event) {
    // Collect partials from window
    partials := o.window.CollectPartials(ev.Symbol)
    
    // Get merged event from pool
    merged := o.mergedEventPool.Get().(*MergedEvent)
    merged.RoutingVersion = atomic.LoadUint64(&o.currentRoutingVersion)
    merged.SourceProviders = extractProviders(partials)
    // ... compose merged data
    
    return merged, partials
}
```

### Validation

```go
func TestOrchestratorRecyclesPartials(t *testing.T) {
    o := setupOrchestrator(t)
    
    // Send 3 partial events
    partials := []*Event{
        {ProviderID: "binance"},
        {ProviderID: "coinbase"},
        {ProviderID: "kraken"},
    }
    
    // Track recycle calls
    recycler := mockRecycler()
    o.recycler = recycler
    
    merged, _ := o.merge(context.Background(), partials[0])
    
    // Verify all partials recycled
    assert.Equal(t, 3, recycler.RecycleManyCallCount())
    assert.NotNil(t, merged)
}
```

---

## Step 3: Dispatcher Parallel Fan-out (conc.Pool)

Replace heap-allocated clones with pool-backed duplicates delivered in parallel.

### Code Example

```go
package dispatcher

import (
    "context"
    "github.com/sourcegraph/conc/pool"
    "github.com/coachpo/meltica/core/recycler"
)

type Dispatcher struct {
    eventPool    *sync.Pool
    recycler     recycler.Recycler
    routingTable map[string][]string // symbol -> subscriber IDs
}

func (d *Dispatcher) Dispatch(ctx context.Context, ev *Event) error {
    subscribers := d.routingTable[ev.Symbol]
    
    if len(subscribers) == 0 {
        // No subscribers, recycle immediately
        d.recycler.RecycleEvent(ev)
        return nil
    }
    
    if len(subscribers) == 1 {
        // Single subscriber, deliver original (no duplication)
        return d.deliverToSubscriber(ctx, subscribers[0], ev)
    }
    
    // Multiple subscribers: parallel fan-out with pooled duplicates
    return d.fanoutParallel(ctx, ev, subscribers)
}

func (d *Dispatcher) fanoutParallel(ctx context.Context, original *Event, subscribers []string) error {
    // Create bounded worker pool
    p := pool.New().WithMaxGoroutines(len(subscribers))
    
    // Spawn parallel delivery workers
    for _, subID := range subscribers {
        sub := subID // Capture loop variable
        p.Go(func() error {
            // Get duplicate from pool
            dup := d.eventPool.Get().(*Event)
            
            // Clone original into duplicate
            d.cloneEvent(original, dup)
            
            // Deliver (consumer wrapper will auto-recycle)
            return d.deliverToSubscriber(ctx, sub, dup)
        })
    }
    
    // Wait for all deliveries (aggregates errors)
    err := p.Wait()
    
    // Recycle original after all duplicates sent
    d.recycler.RecycleEvent(original)
    
    return err
}

func (d *Dispatcher) cloneEvent(src, dst *Event) {
    dst.TraceID = src.TraceID
    dst.RoutingVersion = src.RoutingVersion
    dst.Kind = src.Kind
    dst.Payload = src.Payload
    dst.IngestTS = src.IngestTS
    dst.SeqProvider = src.SeqProvider
    dst.ProviderID = src.ProviderID
}
```

### Validation

```go
func TestDispatcherParallelFanout(t *testing.T) {
    d := setupDispatcher(t)
    
    // 10 subscribers
    d.routingTable["BTC-USDT"] = []string{"sub1", "sub2", ..., "sub10"}
    
    ev := &Event{Symbol: "BTC-USDT", Kind: KindMarketData}
    
    start := time.Now()
    err := d.Dispatch(context.Background(), ev)
    elapsed := time.Since(start)
    
    // Verify parallel delivery (<15ms for 10 subscribers)
    assert.NoError(t, err)
    assert.Less(t, elapsed, 15*time.Millisecond)
    
    // Verify original recycled (mock recycler)
    assert.Equal(t, 1, mockRecycler.RecycleEventCallCount())
}
```

---

## Step 4: Consumer Wrapper (Auto-Recycle + Filtering)

Wrap consumer lambdas to handle recycle, panic recovery, and routing_version filtering.

### Code Example

```go
package consumer

import (
    "context"
    "fmt"
    "runtime/debug"
    "sync/atomic"
    "github.com/coachpo/meltica/core/recycler"
)

type ConsumerWrapper struct {
    consumerID       string
    minAcceptVersion uint64 // Atomic
    recycler         recycler.Recycler
}

func (w *ConsumerWrapper) Invoke(ctx context.Context, ev *Event, lambda ConsumerFunc) (err error) {
    // Auto-recycle on return or panic
    defer func() {
        if p := recover(); p != nil {
            err = fmt.Errorf("consumer panic: %v (stack: %s)", p, debug.Stack())
        }
        w.recycler.RecycleEvent(ev)
    }()
    
    // Pre-gate filter for market-data during flips
    if !w.shouldProcess(ev) {
        return nil // Skip filtered event
    }
    
    // Invoke consumer lambda
    return lambda(ctx, ev)
}

func (w *ConsumerWrapper) shouldProcess(ev *Event) bool {
    // Critical events always processed
    if ev.Kind.IsCritical() {
        return true
    }
    
    // Market data: check routing version
    minVersion := atomic.LoadUint64(&w.minAcceptVersion)
    return ev.RoutingVersion >= minVersion
}

func (w *ConsumerWrapper) UpdateMinVersion(version uint64) {
    atomic.StoreUint64(&w.minAcceptVersion, version)
}
```

### Validation

```go
func TestConsumerWrapperAutoRecycle(t *testing.T) {
    recycler := mockRecycler()
    wrapper := NewConsumerWrapper("test-consumer", recycler)
    
    ev := &Event{Kind: KindMarketData}
    
    // Lambda that panics
    panicLambda := func(ctx context.Context, ev *Event) error {
        panic("test panic")
    }
    
    err := wrapper.Invoke(context.Background(), ev, panicLambda)
    
    // Verify panic captured
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "consumer panic")
    
    // Verify event recycled despite panic
    assert.Equal(t, 1, recycler.RecycleEventCallCount())
}

func TestConsumerWrapperFiltersStaleMarketData(t *testing.T) {
    wrapper := NewConsumerWrapper("test-consumer", mockRecycler())
    wrapper.UpdateMinVersion(100)
    
    staleEvent := &Event{Kind: KindMarketData, RoutingVersion: 50}
    freshEvent := &Event{Kind: KindMarketData, RoutingVersion: 100}
    criticalEvent := &Event{Kind: KindExecReport, RoutingVersion: 50}
    
    assert.False(t, wrapper.shouldProcess(staleEvent))   // Filtered
    assert.True(t, wrapper.shouldProcess(freshEvent))    // Processed
    assert.True(t, wrapper.shouldProcess(criticalEvent)) // Always processed
}
```

---

## Step 5: Debug Mode Usage

Enable debug mode to catch use-after-put and double-put bugs during development.

### Enable Debug Mode

```bash
# Environment variable
export RECYCLER_DEBUG_MODE=true

# Or programmatically
recycler.GlobalRecycler.EnableDebugMode()
```

### Expected Behavior

**Double-Put Detection**:
```go
ev := eventPool.Get().(*Event)
recycler.RecycleEvent(ev)  // First recycle: OK
recycler.RecycleEvent(ev)  // Second recycle: PANIC

// Panic message:
// "double-put detected: 0xc00012a000 (stack: ...)"
```

**Use-After-Put Detection**:
```go
ev := eventPool.Get().(*Event)
recycler.RecycleEvent(ev) // Memory poisoned

_ = ev.TraceID // Access after recycle: PANIC
// Panic message:
// "use-after-put violation: poisoned memory accessed"
```

### Disable in Production

```bash
# Production deployments
export RECYCLER_DEBUG_MODE=false
```

---

## Step 6: Metrics & Observability

Monitor Recycler and Consumer performance via telemetry.

### Code Example

```go
import "github.com/prometheus/client_golang/prometheus"

var (
    recyclerEventsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "recycler_events_total",
            Help: "Total events recycled by type",
        },
        []string{"event_kind"},
    )
    
    consumerProcessingDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "consumer_processing_duration_seconds",
            Help: "Consumer lambda execution time",
            Buckets: prometheus.DefBuckets,
        },
        []string{"consumer_id"},
    )
)

func init() {
    prometheus.MustRegister(recyclerEventsTotal)
    prometheus.MustRegister(consumerProcessingDuration)
}
```

### Query Examples

```promql
# Event recycle rate by type
rate(recycler_events_total[5m])

# Consumer processing latency (p95)
histogram_quantile(0.95, rate(consumer_processing_duration_seconds_bucket[5m]))

# Double-put violations (should be 0 in production)
recycler_double_puts_total
```

---

## Step 7: Migration Checklist

Use this checklist when migrating existing code:

- [ ] Initialize Recycler singleton at startup with pools
- [ ] Enable debug mode in dev/test environments
- [ ] Update Orchestrator to call `RecycleMany(partials)` after merge
- [ ] Update Orchestrator to stamp `RoutingVersion` before Dispatcher
- [ ] Replace Dispatcher heap clones with `pool.Get()` duplicates
- [ ] Migrate Dispatcher worker pools to `github.com/sourcegraph/conc`
- [ ] Recycle original event in Dispatcher after fan-out
- [ ] Wrap consumer lambdas with `ConsumerWrapper.Invoke()`
- [ ] Remove all direct `eventPool.Put()` calls (use Recycler)
- [ ] Remove all `async/pool` imports (banned per PERF-09)
- [ ] Add tests for parallel fan-out latency (<15ms for 10 subscribers)
- [ ] Add tests for goroutine leak detection (goleak package)
- [ ] Add tests for panic recovery in consumers
- [ ] Verify CI checks pass (race detector, coverage ≥70%, banned imports)
- [ ] Run 24-hour soak test to verify zero memory growth

---

## Common Pitfalls

### ❌ Don't: Direct pool.Put()

```go
// WRONG - bypasses Recycler
eventPool.Put(ev)
```

### ✅ Do: Use Recycler

```go
// CORRECT - single return gateway
recycler.GlobalRecycler.RecycleEvent(ev)
```

---

### ❌ Don't: Forget to recycle original after fan-out

```go
// WRONG - memory leak
for _, sub := range subscribers {
    dup := pool.Get().(*Event)
    clone(original, dup)
    deliver(sub, dup)
}
// Original never recycled!
```

### ✅ Do: Recycle after all duplicates sent

```go
// CORRECT
p := pool.New().WithMaxGoroutines(len(subscribers))
for _, sub := range subscribers {
    // ... parallel delivery
}
p.Wait()
recycler.RecycleEvent(original) // Recycle after all done
```

---

### ❌ Don't: Filter critical events

```go
// WRONG - breaks order lifecycle
if ev.RoutingVersion < minAcceptVersion {
    return nil // Skip ALL events (including critical!)
}
```

### ✅ Do: Always deliver critical events

```go
// CORRECT
if ev.Kind.IsCritical() {
    return lambda(ctx, ev) // Always process
}
if ev.RoutingVersion < minAcceptVersion {
    return nil // Skip only market-data
}
```

---

## Testing Strategy

### Unit Tests

```bash
# Run with race detector
go test -race ./core/dispatcher ./core/orchestrator ./core/consumer

# Check coverage (must be ≥70%)
go test -cover ./...
```

### Integration Tests

```bash
# End-to-end fan-out test
go test -v ./tests/integration/fanout_test.go

# Leak detection (goleak)
go test -v -run TestNoGoroutineLeaks ./...
```

### Benchmark Tests

```bash
# Parallel fan-out performance
go test -bench=BenchmarkParallelFanout -benchmem ./core/dispatcher

# Expected: <15ms for 10 subscribers, <80% pool utilization
```

---

## Next Steps

1. **Review Contracts**: Read [contracts/recycler.go](./contracts/recycler.go) for API details
2. **Study Data Model**: See [data-model.md](./data-model.md) for entity relationships
3. **Implementation**: Use this quickstart as reference during coding
4. **Iterate**: Run tests frequently with `-race` flag
5. **Validate**: Check Constitution compliance before merge

---

## Support

- **Spec**: [spec.md](./spec.md)
- **Research**: [research.md](./research.md)
- **Architecture Diagrams**: See spec.md for PlantUML diagrams
- **Constitution**: `.specify/memory/constitution.md` (PERF-06, PERF-07, PERF-08, PERF-09)

**use context7** for latest library documentation when using Cursor or supported agents.

