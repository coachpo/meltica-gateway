# Migration Guide: Performance & Memory Architecture Upgrade

## Overview

This release replaces legacy network/serialization libraries and introduces bounded object pools for hot-path structs. The changes reduce heap allocations, improve WebSocket and JSON performance, and add ownership safeguards during shutdown.

## Library Changes

- **WebSocket:** `github.com/gorilla/websocket` → `github.com/coder/websocket`
  - Dial/Read/Write now accept `context.Context` for deadline management.
  - Close handshake uses `conn.Close(status, reason)`; ping/pong helpers are built-in.

- **JSON:** `encoding/json` → `github.com/goccy/go-json`
  - API is drop-in compatible (Marshal/Unmarshal/Encoder/Decoder) and significantly faster.
  - Alias import as `json "github.com/goccy/go-json"` for minimal code changes.

## Object Pooling

Canonical structs (`Event`, `OrderRequest`) are pooled via `pool.PoolManager` for memory efficiency:

```go
order, release, err := pool.AcquireOrderRequest(ctx, pools)
if err != nil {
    return err
}
defer release()

// populate the order and forward it downstream
order.ClientOrderID = id
```

- Acquisition uses a 100ms timeout (`context.WithTimeout`).
- Double `Put()` calls panic with a captured stack trace.
- Debug builds poison returned objects and record acquisition stacks for leak analysis.
- `PoolManager.Shutdown(ctx)` waits up to 5 seconds for in-flight objects and logs outstanding stacks on timeout.

## CI Enforcement

`golangci-lint` `depguard` rules fail the build when `encoding/json` or `github.com/gorilla/websocket` are imported. Run `make lint` before committing to catch violations locally.

## Testing & Benchmarks

- `tests/unit/websocket_migration_test.go` contains functional tests and a regression benchmark for the new WebSocket client.
- `tests/unit/json_migration_test.go` verifies JSON equivalence and measures marshal/unmarshal speedups.
- `tests/integration/latency_bench_test.go` exercises the end-to-end pipeline and asserts p99 latency targets under pooled operation.

## Action Items

1. Update custom providers or consumers to request pooled objects through `pool.PoolManager` helpers.
2. Replace remaining `encoding/json` or `gorilla/websocket` references in downstream code.
3. Run `golangci-lint run --config .golangci.yml` to validate depguard rules.
4. Execute `go test ./... -race` to ensure the new safety instrumentation passes race detection.
