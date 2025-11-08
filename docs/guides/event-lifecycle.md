# Meltica Event Lifecycle

This document explains how a `schema.Event` travels through Meltica—from the moment a provider emits data to the point strategies consume and recycle it. Use it as the canonical reference when debugging routing, pooling, or observability issues.

## 1. Canonical Event Structure & Pooling

- **Definition**: `internal/domain/schema/event.go` describes the `Event` struct (provider, symbol, `EventType`, provider sequence, ingest/emit timestamps, payload, ownership flag).
- **Pooling**: `internal/infra/pool/manager.go` hands out reusable event instances through `BorrowEventInst/ReturnEventInst`. Pools enforce zero-copy flows; failing to return an event leaks capacity and eventually stalls adapters.
- **Deep copy helper**: `schema.CopyEvent` (`internal/domain/schema/clone.go`) duplicates payloads when the bus needs per-subscriber clones.

## 2. Provider Emission

1. **Adapters**: Exchange-specific adapters embed `shared.Publisher` (`internal/infra/adapters/shared/publisher.go`).
2. **Borrow & populate**: The publisher borrows an event, sets `EventID` (provider+symbol+type+seq), stamps timestamps, and attaches the typed payload.
3. **Emit**: Events are written onto the provider instance’s `Events()` channel (`internal/app/provider/provider.go`). Subscription activation is coordinated via the dispatcher registrar + shared `SubscriptionManager`, so only declared routes generate upstream traffic.

## 3. Provider Manager → Dispatcher Runtime

- The provider manager wires each running instance to a dispatcher runtime (`internal/app/provider/manager.go`, `startProviderRuntime`).
- `dispatcher.Runtime` (`internal/app/dispatcher/runtime.go`):
  - pulls events off the provider channel;
  - attaches the current routing table revision (`table.Version()`);
  - ensures `EmitTS` is populated;
  - deduplicates via `EventID` to stop replay storms;
  - publishes the surviving event to the data bus.
- Duplicates or rejected events are returned to the pool immediately (`releaseEvent`).

## 4. Event Bus Fan-out

- Implementation: `internal/infra/bus/eventbus/memory.go`.
- Flow:
  1. Snapshot subscribers for `evt.Type`. If none, recycle the source event (cheap short-circuit).
  2. Borrow `n` clones from the pool (`borrowBatchForFanout`) and `schema.CopyEvent` into each.
  3. Dispatch using a bounded worker pool; each subscriber gets a dedicated clone. Delivery failures recycle the clone via `deliverWithRecycle` and increment delivery error metrics.
  4. After fan-out, recycle the original event.
- Metrics (publish duration, fanout size, delivery errors) feed Grafana dashboards to catch backpressure.

## 5. Lambda Consumption

- `core.BaseLambda` (`internal/app/lambda/core/base.go`) subscribes to all event types required by the strategy (`bus.Subscribe`).
- Each subscription runs in its own goroutine:
  - Filters by provider/symbol (balance events are filtered by currency sets).
  - Decodes payload into the typed structs (trade, ticker, book snapshot, exec report, balance, risk control, extension).
  - Invokes the strategy callback (`TradingStrategy` interface) and updates shared state (last price, risk manager cues, persisted orders/balances).
- After handling, `recycleEvent` returns the instance to the pool, keeping object churn minimal.

## 6. Emitting Control Events from Lambdas

- Lambdas can publish their own canonical events (e.g., risk control alerts) by borrowing a fresh event, populating it, and calling `bus.Publish` (`BaseLambda.emitRiskControlEvent`). These events re-enter the same lifecycle (bus fan-out → subscribers).

## 7. Key Failure Modes & Checks

| Stage | Symptom | Check |
| --- | --- | --- |
| Provider emission | Events stop suddenly | Verify adapters still borrow from pools; inspect provider logs for `borrow canonical event failed`. |
| Dispatcher runtime | Duplicate drop surge | Look for non-unique `EventID` construction or replayed upstream data. |
| Event bus | Delivery errors spike | Inspect `delivery_blocked` metric and subscriber buffering; ensure lambdas drain channels. |
| Lambda consumption | Pool exhaustion warning | Confirm strategies always call `recycleEvent`; check for goroutines stuck on filters. |

Understanding this lifecycle ensures every event is routed deterministically, observability metrics stay accurate, and pooled resources recycle correctly.
