# Research & Technology Decisions: Monolithic Auto-Trading Application

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)  
**Date**: October 12, 2025  
**Status**: Complete

## Executive Summary

This document resolves all technology and pattern decisions for the monolithic auto-trading application. All choices prioritize Go idioms, simplicity, and deterministic testing over premature optimization.

---

## 1. Concurrency Pattern for Windowed Merge

**Decision**: Use channels with time.Ticker and counting logic

**Rationale**:
- Go's `time.Ticker` provides reliable time-based window closure
- Channel-based counting allows event-driven closure (1000 events)
- Idiomatic Go: goroutines + channels + select for coordination
- Testable with time injection (`time.Now` function pointer for deterministic tests)

**Alternatives Considered**:
- **Mutex-based state machine**: More complex error handling, harder to test deterministically
- **Third-party workflow engine**: Overkill for simple time+count windowing; adds external dependency

**Implementation Approach**:
```go
type MergeWindow struct {
    mergeID     string
    openTime    time.Time
    events      []Event
    eventCount  int
    providers   map[string]bool
    timer       *time.Timer
    eventCh     chan Event
    closeCh     chan struct{}
}
```

Window closes on:
1. `time.After(10 * time.Second)` fires → check completeness → emit or suppress
2. Event count reaches 1000 → immediate closure → emit or suppress

**Testing Strategy**: Inject `timeNow` function and use fake time in tests; verify window behavior under all scenarios (complete, partial, late).

---

## 2. Per-Stream Ordering Algorithm

**Decision**: Priority queue (heap) sorted by seq_provider with ingest_ts tiebreaker

**Rationale**:
- Go's `container/heap` provides O(log n) insertions and extractions
- Natural fit for reordering out-of-order events within lateness window
- Small buffer (200 events max per stream) keeps heap operations fast
- Flush timer (50ms) ensures bounded latency even with sparse events

**Alternatives Considered**:
- **Sorted slice with binary search**: O(n) insertions, acceptable for small buffers but less idiomatic
- **Map-based buffering**: Requires custom sorting on flush; no ordering guarantees during insertion

**Implementation Approach**:
```go
type StreamBuffer struct {
    streamKey   StreamKey // (provider, symbol, eventType)
    heap        *EventHeap // container/heap sorted by seq_provider, then ingest_ts
    lastEmitted uint64     // seq_provider of last emitted event
    flushTimer  *time.Timer // 50ms
    maxBuffer   int         // 200
}
```

Events arriving with `seq_provider <= lastEmitted` are duplicates → drop.  
Events arriving with `seq_provider > lastEmitted + maxBuffer` are too late → drop and log telemetry.

**Testing Strategy**: Inject events out-of-order (e.g., seq 100, 103, 101, 102); verify output order (100, 101, 102, 103); test gap handling and late-event drops.

---

## 3. Token-Bucket Rate Limiting

**Decision**: Use `golang.org/x/time/rate` Limiter per stream

**Rationale**:
- Standard library-quality implementation with `Allow()`, `Wait()`, `Reserve()` methods
- Per-stream token buckets enable fair-share allocation across consumers
- Built-in burst capacity allows natural traffic spikes without immediate backpressure
- Well-tested, production-ready, no need to implement custom rate limiter

**Alternatives Considered**:
- **Custom token bucket**: Reinventing the wheel; error-prone concurrency
- **Fixed-window rate limiting**: Less smooth than token bucket; can cause bursty rejections

**Implementation Approach**:
```go
type StreamLimiter struct {
    streamKey StreamKey
    limiter   *rate.Limiter // rate.NewLimiter(ratePerSecond, burst)
}
```

On event dispatch:
1. Check `limiter.Allow()` → if false, apply coalescing (if coalescable) or queue (if ExecReport)
2. If coalescable event and overflow → drop oldest, keep latest (latest-wins)
3. If ExecReport and overflow → queue without dropping (lossless guarantee)

**Testing Strategy**: Simulate high event rate; verify token bucket allows fair throughput; test coalescing triggers under backpressure; verify ExecReport never dropped.

---

## 4. WebSocket Client Library

**Decision**: `gorilla/websocket` (already in use by existing Binance adapter)

**Rationale**:
- Already in codebase (`internal/adapters/binance/ws_client.go`)
- Mature, widely adopted, well-documented
- Supports compression, custom headers, and ping/pong for keepalive
- Consistent with existing patterns; no refactoring needed

**Alternatives Considered**:
- **nhooyr.io/websocket**: Newer, supports wasm, but no compelling benefit for server-side monolith
- **Standard library WebSocket**: Does not exist; net/http supports upgrade but no high-level client

**Implementation Approach**: Extend existing `internal/adapters/binance/ws_client.go` pattern to Coinbase and Kraken providers. Each provider module owns its WebSocket connection lifecycle (connect, reconnect, disconnect).

**Testing Strategy**: Use fake WebSocket server (`httptest.Server` with upgrade) in unit tests; verify reconnection logic, gap detection, and clean shutdown.

---

## 5. Checksum Algorithm for Order Book Verification

**Decision**: Use provider-specific checksum algorithms (CRC32 for Binance, SHA256 for Coinbase, etc.)

**Rationale**:
- Each exchange provides checksums in their own format (Binance: CRC32 of concatenated price levels; Coinbase: SHA256 hash)
- Reusing provider checksums avoids introducing validation drift
- Checksums are advisory; on failure, trigger REST snapshot refresh (fail-safe)

**Alternatives Considered**:
- **Custom checksum**: Would not match provider's expected value; creates false positives
- **No checksum verification**: Misses data corruption; violates LM-07 requirement

**Implementation Approach**:
```go
type BookAssembler interface {
    ApplyDiff(diff BookDiff) error
    VerifyChecksum(expected string) error
    RefreshSnapshot() error
}
```

Each provider adapter implements `VerifyChecksum` using exchange-specific algorithm. On mismatch:
1. Log telemetry: `book.resync{provider, symbol, reason=checksum}`
2. Discard current book state
3. Fetch REST snapshot
4. Resume applying diffs from snapshot sequence number

**Testing Strategy**: Unit tests with known good/bad checksums; verify resync triggered on mismatch; integration test with fake provider emitting intentional checksum failures.

---

## 6. Latest-Wins Coalescing Strategy

**Decision**: Map-based with last-write-wins (keyed by stream key)

**Rationale**:
- Simple `map[StreamKey]*Event` where newest event replaces older event
- O(1) insertion and retrieval
- Natural fit for coalescable events (Ticker, Book, KlineSummary) where only latest state matters
- Clear semantic: "If you have 10 book updates in 50ms, only the newest is delivered"

**Alternatives Considered**:
- **Ring buffer**: More complex; fixed size leads to wraparound logic; no performance benefit
- **Sliding window deduplication**: Retains all events within window; defeats purpose of coalescing

**Implementation Approach**:
```go
type Coalescer struct {
    coalescableTypes map[EventType]bool // {Ticker: true, Book: true, KlineSummary: true}
    latestEvents     map[StreamKey]*Event
    mu               sync.RWMutex
}

func (c *Coalescer) Coalesce(event *Event) (*Event, bool) {
    if !c.coalescableTypes[event.Type] {
        return event, false // Not coalescable (e.g., ExecReport)
    }
    c.mu.Lock()
    defer c.mu.Unlock()
    key := StreamKey{event.Provider, event.Symbol, event.Type}
    old := c.latestEvents[key]
    c.latestEvents[key] = event
    return event, old != nil // Return true if replaced an old event
}
```

Flush on timer (50ms): deliver all entries in `latestEvents` map, then clear map.

**Testing Strategy**: Inject 10 Ticker events for same symbol in rapid succession; verify only latest delivered; inject ExecReport events → verify all delivered (no coalescing).

---

## 7. Testing Strategies for Race-Free Concurrent Code

**Decision**: Use Go's race detector (`-race`), context-based cancellation, and WaitGroups for deterministic shutdown

**Rationale**:
- Race detector catches data races at runtime; mandatory in CI (TS-03)
- Context propagation (`context.WithCancel`, `context.WithTimeout`) provides clean cancellation signal
- `sync.WaitGroup` ensures all goroutines complete before test teardown
- Fake time (`time.Now` function pointer) makes timer-based tests deterministic

**Alternatives Considered**:
- **Sleeps in tests**: Non-deterministic; brittle; fails intermittently under load (violates TS-04)
- **Global state without locking**: Causes races; fails `-race` detector

**Implementation Approach**:
- All components accept `context.Context` as first parameter
- All goroutines decrement WaitGroup on exit: `defer wg.Done()`
- Tests use `context.WithTimeout` (e.g., 5 seconds) to prevent hangs
- Mock time in tests using interface: `type Clock interface { Now() time.Time }`

**Testing Strategy**:
```go
func TestMergeWindow_Deterministic(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    fakeClock := &FakeClock{currentTime: time.Unix(0, 0)}
    orchestrator := NewOrchestrator(WithClock(fakeClock))
    
    // Inject events, advance fake time, verify window closure
    orchestrator.IngestEvent(ctx, event1)
    fakeClock.Advance(10 * time.Second)
    orchestrator.IngestEvent(ctx, event2) // Should trigger window closure
    
    // Assert window closed, emitted, or suppressed
}
```

All tests run with `go test ./... -race -count=1 -timeout=30s` in CI.

---

## 8. Configuration Format for Merge Rules and Provider Settings

**Decision**: Extend existing YAML format (`config/streaming.yaml`) with merge rules and provider configurations

**Rationale**:
- Codebase already uses YAML for configuration (`config/streaming.example.yaml`)
- Go's `gopkg.in/yaml.v3` supports nested structures and type-safe unmarshaling
- Declarative format is readable and supports comments for operational context
- Existing `config/config.go` loader can be extended without disruption

**Alternatives Considered**:
- **TOML**: Less familiar to existing codebase; no significant benefit
- **JSON**: No comments support; harder to document configuration inline
- **Code-based config**: Loses declarative benefits; requires recompilation for config changes

**Implementation Approach**:
```yaml
# config/streaming.yaml
providers:
  - name: binance
    ws_endpoint: wss://stream.binance.com:9443/ws
    rest_endpoint: https://api.binance.com
    symbols: ["BTC-USDT", "ETH-USDT", "SOL-USDT"]
    book_refresh_interval: 3m
    
  - name: coinbase
    ws_endpoint: wss://ws-feed.exchange.coinbase.com
    rest_endpoint: https://api.exchange.coinbase.com
    symbols: ["BTC-USD", "ETH-USD"]
    book_refresh_interval: 2m

orchestrator:
  merge_rules:
    - merge_key: "BTC-USDT:Book"
      providers: ["binance", "coinbase"]
      window_duration: 10s
      window_max_events: 1000
      partial_policy: suppress
      
dispatcher:
  stream_ordering:
    lateness_tolerance: 150ms
    flush_interval: 50ms
    max_buffer_size: 200
  backpressure:
    token_rate_per_stream: 1000  # events per second
    token_burst: 100
  coalescable_types: ["Ticker", "Book", "KlineSummary"]

consumers:
  - name: strategy_alpha
    trading_switch: enabled
    subscriptions:
      - symbol: "BTC-USDT"
        providers: ["binance"]
      - symbol: "ETH-USDT"
        providers: ["binance", "coinbase"]
        merged: true
```

Load with existing config loader, extend structs in `config/config.go`:
```go
type Config struct {
    Providers   []ProviderConfig   `yaml:"providers"`
    Orchestrator OrchestratorConfig `yaml:"orchestrator"`
    Dispatcher  DispatcherConfig   `yaml:"dispatcher"`
    Consumers   []ConsumerConfig   `yaml:"consumers"`
}
```

**Testing Strategy**: Unit tests with sample YAML; verify parsing; test missing required fields return errors; integration test loads full config and initializes components.

---

## Technology Stack Summary

| Component | Technology | Justification |
|-----------|------------|---------------|
| Language | Go 1.25 | Idiomatic concurrency, existing codebase |
| WebSocket Client | gorilla/websocket | Already in use, mature, well-tested |
| Rate Limiting | golang.org/x/time/rate | Standard library quality, token-bucket |
| Ordering | container/heap | O(log n) priority queue, idiomatic |
| Configuration | YAML (gopkg.in/yaml.v3) | Existing format, readable, comments |
| Testing | Go test + testify + mocks | Built-in test framework, -race support |
| Telemetry | Existing lib/telemetry/otel.go | Already integrated OpenTelemetry |

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Race conditions in concurrent code | Mandatory `-race` in all tests (CI); WaitGroups for shutdown; context cancellation |
| Non-deterministic test failures | Inject time (`Clock` interface); no sleeps; context timeouts |
| WebSocket reconnection storms | Exponential backoff; jittered delays; max retry limits |
| Order book corruption | Checksum verification; automatic REST snapshot refresh; telemetry alerts |
| Backpressure cascade failures | Token-bucket rate limiting; fair-share allocation; coalescable event dropping |
| Configuration drift | Validate config on startup; fail-fast on missing required fields; schema versioning |

---

## Open Questions: None

All technology decisions resolved. Ready for Phase 1 (Design).

