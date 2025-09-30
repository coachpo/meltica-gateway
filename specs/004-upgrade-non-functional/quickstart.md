# Quickstart: Performance & Memory Architecture Upgrade

**Feature**: 004-upgrade-non-functional  
**Target Audience**: Developers implementing library replacements and object pooling  
**Prerequisites**: Go 1.25+, existing Meltica monolith codebase

## Overview

This guide walks through using the upgraded performance and memory architecture with:
1. **Library replacements**: coder/websocket (replaces gorilla/websocket), goccy/go-json (replaces encoding/json)
2. **Object pooling**: sync.Pool for 6 hot-path struct types with Reset() methods
3. **Ownership rules**: Dispatcher creates heap clones for fan-out; consumers own clones until GC

**Expected Outcomes**:
- 40% reduction in heap allocations
- <150ms p99 end-to-end latency
- 30% faster JSON parsing
- Zero memory leaks over 24hr operation

---

## Step 1: Install Dependencies

Update `go.mod` with new libraries:

```bash
# Add new dependencies
go get github.com/coder/websocket@latest
go get github.com/goccy/go-json@latest

# Remove old dependency (if not used transitively)
go mod tidy
```

Verify imports are banned in CI:

```yaml
# .golangci.yml
linters:
  enable:
    - depguard

linters-settings:
  depguard:
    rules:
      banned-libs:
        deny:
          - pkg: "encoding/json"
            desc: "Use github.com/goccy/go-json instead"
          - pkg: "github.com/gorilla/websocket"
            desc: "Use github.com/coder/websocket instead"
```

---

## Step 2: Replace WebSocket Client (coder/websocket)

### Before (gorilla/websocket)

```go
import "github.com/gorilla/websocket"

func connectToExchange(url string) (*websocket.Conn, error) {
    conn, _, err := websocket.DefaultDialer.Dial(url, nil)
    if err != nil {
        return nil, err
    }
    
    conn.SetReadDeadline(time.Now().Add(30 * time.Second))
    return conn, nil
}

func readMessage(conn *websocket.Conn) ([]byte, error) {
    _, msg, err := conn.ReadMessage()
    return msg, err
}
```

### After (coder/websocket)

```go
import "github.com/coder/websocket"

func connectToExchange(ctx context.Context, url string) (*websocket.Conn, error) {
    conn, _, err := websocket.Dial(ctx, url, nil)
    if err != nil {
        return nil, err
    }
    return conn, nil
}

func readMessage(ctx context.Context, conn *websocket.Conn) ([]byte, error) {
    // Timeout is managed via context, not SetReadDeadline
    readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()
    
    _, msg, err := conn.Read(readCtx)
    return msg, err
}
```

**Key Changes**:
- Pass `context.Context` to all operations (Dial, Read, Write)
- Timeouts controlled via context, not SetReadDeadline
- Close codes handled natively (no type assertion needed)

---

## Step 3: Replace JSON Marshaling (goccy/go-json)

### Before (encoding/json)

```go
import "encoding/json"

func parseEvent(data []byte) (*Event, error) {
    var event Event
    if err := json.Unmarshal(data, &event); err != nil {
        return nil, err
    }
    return &event, nil
}

func serializeEvent(event *Event) ([]byte, error) {
    return json.Marshal(event)
}
```

### After (goccy/go-json)

```go
import json "github.com/goccy/go-json"  // Alias as "json" for drop-in replacement

func parseEvent(data []byte) (*Event, error) {
    var event Event
    if err := json.Unmarshal(data, &event); err != nil {  // Same API
        return nil, err
    }
    return &event, nil
}

func serializeEvent(event *Event) ([]byte, error) {
    return json.Marshal(event)  // Same API
}
```

**Key Changes**:
- Import path changed; alias as `json` for minimal code changes
- API is identical (Marshal/Unmarshal, Encoder/Decoder)
- Performance improvement is automatic (no code changes)

---

## Step 4: Initialize Pool Manager

Create centralized pool manager at application startup:

```go
// main.go or initialization code
import "github.com/yourorg/meltica/internal/pool"

func main() {
    // Create pool manager
    poolMgr := pool.NewPoolManager()
    
    // Register pools for each struct type
    poolMgr.RegisterPool("WsFrame", 200, func() interface{} {
        return &schema.WsFrame{}
    })
    
    poolMgr.RegisterPool("ProviderRaw", 200, func() interface{} {
        return &schema.ProviderRaw{}
    })
    
    poolMgr.RegisterPool("CanonicalEvent", 300, func() interface{} {
        return &schema.CanonicalEvent{}
    })
    
    poolMgr.RegisterPool("MergedEvent", 50, func() interface{} {
        return &schema.MergedEvent{}
    })
    
    poolMgr.RegisterPool("OrderRequest", 20, func() interface{} {
        return &schema.OrderRequest{}
    })
    
    poolMgr.RegisterPool("ExecReport", 20, func() interface{} {
        return &schema.ExecReport{}
    })
    
    // Pass poolMgr to components that use pooled objects
    orchestrator := conductor.NewOrchestrator(poolMgr)
    dispatcher := dispatcher.NewDispatcher(poolMgr)
    
    // Graceful shutdown
    defer func() {
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        
        if err := poolMgr.Shutdown(shutdownCtx); err != nil {
            log.Printf("Pool shutdown warning: %v", err)
        }
    }()
    
    // Start application...
}
```

---

## Step 5: Use Pooled Objects (Get/Put Pattern)

### WebSocket Frame Reading (WsFrame)

```go
// WebSocket client in provider adapter
func (wc *WsClient) readLoop(ctx context.Context) {
    for {
        // Get pooled WsFrame
        getCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
        obj, err := wc.poolMgr.Get("WsFrame", getCtx)
        cancel()
        
        if err != nil {
            // Pool exhausted (backpressure)
            log.Printf("WsFrame pool exhausted: %v", err)
            continue
        }
        
        frame := obj.(*schema.WsFrame)
        defer wc.poolMgr.Put("WsFrame", frame)  // Ensure Put() on all paths
        
        // Read WebSocket frame
        _, data, err := wc.conn.Read(ctx)
        if err != nil {
            return  // Connection closed; frame Put() via defer
        }
        
        // Populate frame
        frame.Provider = wc.providerName
        frame.ConnID = wc.connID
        frame.ReceivedAt = time.Now().UnixNano()
        frame.MessageType = websocket.MessageText
        frame.Data = data
        
        // Parse into ProviderRaw (next pool stage)
        wc.parseFrame(ctx, frame)
        
        // frame Put() happens via defer after parseFrame returns
    }
}
```

### Event Normalization (ProviderRaw → CanonicalEvent)

```go
func (p *Parser) normalize(ctx context.Context, raw *schema.ProviderRaw) {
    // Get pooled CanonicalEvent
    getCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
    obj, err := p.poolMgr.Get("CanonicalEvent", getCtx)
    cancel()
    
    if err != nil {
        log.Printf("CanonicalEvent pool exhausted: %v", err)
        return
    }
    
    canonical := obj.(*schema.CanonicalEvent)
    defer p.poolMgr.Put("CanonicalEvent", canonical)
    
    // Normalize provider-specific payload to canonical schema
    canonical.Provider = raw.Provider
    canonical.Symbol = p.normalizeSymbol(raw.StreamName)
    canonical.EventType = schema.EventTypeTrade
    canonical.IngestTS = time.Now().UnixNano()
    canonical.Data = raw.Payload  // Shallow copy (JSON bytes)
    canonical.TraceID = generateTraceID()
    
    // Route to Dispatcher (takes ownership temporarily)
    p.dispatcher.Ingest(ctx, canonical)
    
    // canonical Put() happens via defer after Ingest returns
}
```

---

## Step 6: Implement Dispatcher Fan-Out with Heap Clones

Dispatcher creates heap clones (not pooled) for each subscriber:

```go
// Dispatcher fan-out logic
func (d *Dispatcher) fanOut(ctx context.Context, pooledEvent *schema.CanonicalEvent) {
    // Create heap clone for each subscriber
    for _, subscriber := range d.subscribers[pooledEvent.Symbol] {
        // Allocate clone on heap (NOT from pool)
        clone := &schema.CanonicalEvent{
            // Shallow copy immutable fields
            Provider:   pooledEvent.Provider,
            Symbol:     pooledEvent.Symbol,
            EventType:  pooledEvent.EventType,
            IngestTS:   pooledEvent.IngestTS,
            ExchangeTS: pooledEvent.ExchangeTS,
            
            // Deep copy mutable slice
            Data: append([]byte(nil), pooledEvent.Data...),
            
            // Shallow copy observability
            TraceID:    pooledEvent.TraceID,
            DecisionID: pooledEvent.DecisionID,
        }
        
        // Enqueue clone to subscriber (non-blocking if queue has space)
        select {
        case subscriber.queue <- clone:
            // Success
        default:
            // Backpressure: apply latest-wins coalescing (if event is coalescable)
            if isCoalescable(pooledEvent.EventType) {
                // Dequeue old event, enqueue new clone
                <-subscriber.queue
                subscriber.queue <- clone
            }
        }
    }
    
    // After all clones enqueued, Put() original back to pool
    d.poolMgr.Put("CanonicalEvent", pooledEvent)
}
```

**Ownership Rules**:
- **Original** (pooled): Dispatcher owns until Put()
- **Clones** (heap): Consumers own until GC (no Put() needed)

---

## Step 7: Consumer Handler (Owns Heap Clone)

Consumer receives heap clone and uses it without calling Put():

```go
func (c *Consumer) handleEvent(event *schema.CanonicalEvent) {
    // event is a heap clone (not pooled)
    // Consumer owns it until handler returns
    
    // Process event
    switch event.EventType {
    case schema.EventTypeTrade:
        c.processTrade(event)
    case schema.EventTypeBookUpdate:
        c.processBook(event)
    }
    
    // event goes out of scope; GC reclaims it
    // NO Put() call needed (would panic if attempted)
}

func (c *Consumer) Start(ctx context.Context) {
    for {
        select {
        case event := <-c.queue:
            c.handleEvent(event)  // Heap clone, owned by consumer
        case <-ctx.Done():
            return
        }
    }
}
```

---

## Step 8: Implement Reset() Methods

Each pooled struct must implement Reset():

```go
// schema/event.go
func (e *CanonicalEvent) Reset() {
    e.returned = false  // Clear ownership flag
    
    // Zero all fields
    e.Provider = ""
    e.Symbol = ""
    e.EventType = 0
    e.SeqProvider = 0
    e.IngestTS = 0
    e.ExchangeTS = 0
    
    // Release slice/map references (allow GC)
    e.Data = nil
    e.Metadata = nil
    
    // Clear strings
    e.TraceID = ""
    e.DecisionID = ""
}

// Unit test to verify Reset() completeness
func TestCanonicalEventReset(t *testing.T) {
    event := &CanonicalEvent{
        Provider:   "binance",
        Symbol:     "BTC-USDT",
        EventType:  1,
        IngestTS:   123456,
        Data:       []byte("test"),
        Metadata:   map[string]string{"key": "value"},
        TraceID:    "trace-123",
    }
    
    event.Reset()
    
    // Verify all fields are zero values
    assert.Zero(t, event.Provider)
    assert.Zero(t, event.Symbol)
    assert.Zero(t, event.EventType)
    assert.Zero(t, event.IngestTS)
    assert.Nil(t, event.Data)
    assert.Nil(t, event.Metadata)
    assert.Zero(t, event.TraceID)
}
```

---

## Step 9: Handle Pool Exhaustion (Backpressure)

When Get() times out (pool exhausted), treat as backpressure signal:

```go
func (d *Dispatcher) Ingest(ctx context.Context, event *schema.CanonicalEvent) error {
    getCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
    obj, err := d.poolMgr.Get("CanonicalEvent", getCtx)
    cancel()
    
    if err == context.DeadlineExceeded {
        // Pool exhausted - backpressure handling
        if isCoalescable(event.EventType) {
            // Drop coalescable event (latest-wins)
            log.Printf("Dropped coalescable event due to backpressure: %s %s", event.Provider, event.Symbol)
            return nil
        } else {
            // NEVER drop ExecReport - block until available
            obj, err = d.poolMgr.Get("CanonicalEvent", ctx)  // No timeout
            if err != nil {
                return fmt.Errorf("failed to get CanonicalEvent: %w", err)
            }
        }
    }
    
    canonical := obj.(*schema.CanonicalEvent)
    defer d.poolMgr.Put("CanonicalEvent", canonical)
    
    // ... process event
}
```

---

## Step 10: Graceful Shutdown

Ensure all pooled objects are returned before shutdown:

```go
func main() {
    poolMgr := pool.NewPoolManager()
    // ... register pools, start application ...
    
    // Graceful shutdown
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh
    
    log.Println("Shutting down...")
    
    // Stop accepting new requests
    cancelApplicationContext()
    
    // Wait for in-flight pooled objects to be returned (5s timeout)
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    if err := poolMgr.Shutdown(shutdownCtx); err != nil {
        log.Printf("Warning: Pool shutdown incomplete: %v", err)
        // Log indicates unreturned objects; investigate missing Put() calls
    } else {
        log.Println("All pooled objects returned successfully")
    }
}
```

---

## Verification Checklist

After implementing the upgrade, verify:

- [ ] **Library Replacement**:
  - [ ] No `encoding/json` imports (CI fails if found)
  - [ ] No `gorilla/websocket` imports (CI fails if found)
  - [ ] All JSON ops use `goccy/go-json`
  - [ ] All WebSocket ops use `coder/websocket`

- [ ] **Object Pooling**:
  - [ ] All 6 struct types have Reset() methods
  - [ ] Reset() unit tests pass for all types (verify fields zeroed)
  - [ ] Pool capacity configured (200 for WsFrame/ProviderRaw, 300 for CanonicalEvent, etc.)
  - [ ] Get() uses 100ms timeout context
  - [ ] Put() called on all paths (use defer to guarantee)

- [ ] **Ownership Rules**:
  - [ ] Dispatcher clones events for subscribers (heap allocation, not pooled)
  - [ ] Dispatcher Put()s original after fan-out
  - [ ] Consumers do NOT call Put() on clones
  - [ ] Deep copy for mutable fields (Data slices), shallow for immutable

- [ ] **Error Handling**:
  - [ ] Pool exhaustion triggers backpressure (drop coalescable, block for ExecReport)
  - [ ] Double-Put() panics with stack trace (detected in tests)
  - [ ] Graceful shutdown waits up to 5s for in-flight objects

- [ ] **Testing**:
  - [ ] All tests pass with `-race` flag
  - [ ] Coverage ≥70%
  - [ ] Benchmarks show ≥40% allocation reduction
  - [ ] End-to-end latency p99 <150ms
  - [ ] 24hr leak test passes (no memory growth)

---

## Troubleshooting

### Issue: "Pool exhaustion" logs during load

**Cause**: Pool capacity too low for current load  
**Solution**: Increase pool capacity (e.g., 200 → 400 for WsFrame) or optimize Put() call sites to return objects faster

### Issue: "Double-Put() detected" panic

**Cause**: Put() called twice on same object  
**Solution**: Review code paths for duplicate Put() calls; ensure defer Put() only on successful Get()

### Issue: Memory leak after 24hr run

**Cause**: Consumer retaining reference to clone beyond handler scope  
**Solution**: Audit consumer handlers; ensure no global/long-lived references to event clones

### Issue: CI fails with "import 'encoding/json' not allowed"

**Cause**: Legacy encoding/json import still present  
**Solution**: Replace with `import json "github.com/goccy/go-json"`; run `golangci-lint run` locally

---

## Performance Benchmarks

Run benchmarks to validate targets:

```bash
# Baseline (before upgrade)
go test -bench=BenchmarkPipeline -benchmem -count=5 > baseline.txt

# After upgrade
go test -bench=BenchmarkPipeline -benchmem -count=5 > upgraded.txt

# Compare
benchstat baseline.txt upgraded.txt
```

**Expected Improvements**:
- 40% fewer allocations (`allocs/op`)
- 30% faster JSON parsing (`ns/op` for JSON-heavy benchmarks)
- 20% lower WebSocket overhead (`bytes/op` for WS frame handling)

---

## Next Steps

1. Implement library replacements (separate PRs for WebSocket and JSON)
2. Implement pooling infrastructure (`internal/pool/` package)
3. Add Reset() methods to all 6 struct types
4. Update components to use pooling (WS client → Parser → Orchestrator → Dispatcher)
5. Run tests with `-race` and validate ≥70% coverage
6. Run 24hr load test to confirm zero memory leaks
7. Merge to main after all success criteria met

