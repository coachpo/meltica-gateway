# Data Model: Dispatcher-Conductor Streaming Refactor

## Canonical Schema

### `schema.CanonicalType`
- Alias of `string` representing a canonical Meltica event category (`ticker`, `orderbook.snapshot`, `orderbook.delta`, `analytics.fused`, etc.).
- Validation: uppercase segments separated by dots, mapped from dispatcher configuration.

### `schema.MelticaEvent`
| Field | Type | Description |
| --- | --- | --- |
| `Type` | `CanonicalType` | Canonical event category originating from dispatcher routing. |
| `Source` | `string` | Ingress source identifier (`binance.ws.ticker`, `binance.rest.orderbook`, `conductor.analytics`). |
| `Ts` | `time.Time` | Exchange event timestamp normalized to UTC. |
| `Instrument` | `string` | Canonical `BASE-QUOTE` symbol (`BTC-USDT`). |
| `Market` | `string` | Market venue (e.g., `BINANCE-SPOT`). |
| `Seq` | `uint64` | Monotonic per-instrument sequence number maintained post-dispatch. |
| `Key` | `string` | Idempotency key (typically `{Instrument}:{Type}:{Seq}`). |
| `Payload` | `any` | Canonical payload matching event subtype schema (ticker, orderbook levels, analytics summary). |
| `Latency` | `time.Duration` | Processing latency from raw ingress to publication. |
| `TraceID` | `string` | OpenTelemetry trace identifier propagated end-to-end. |

Validation rules:
- `Instrument` MUST follow `BASE-QUOTE` uppercase format (CQ-04).
- `Seq` increases monotonically per `(Instrument, Type)`; duplicates discarded by Data Bus consumers.
- `Payload` structures versioned via dispatcher configuration; unknown fields rejected by dispatcher canonicalizer.

### `schema.RawInstance`
- Type alias of `map[string]any` produced by the Binance adapter before dispatcher filtering.
- Retains exchange-native keys but normalized for casing and numeric encoding.

## Dispatcher Domain

### `dispatcher.DispatchTable`
- Type: `map[schema.CanonicalType]Route`.
- Populated by control-plane commands and boot-time configuration.
- Guarantees single source of truth for routing decisions (EP-03).

### `dispatcher.Route`
| Field | Type | Description |
| --- | --- | --- |
| `WSTopics` | `[]string` | Binance native WS topics to subscribe for this canonical type. |
| `RESTFns` | `[]RestFn` | REST pollers emitting snapshots for the canonical type. |
| `Filters` | `[]FilterRule` | Filter predicates applied to `RawInstance` before canonicalization. |

### `dispatcher.RestFn`
| Field | Type | Description |
| --- | --- | --- |
| `Name` | `string` | Identifier for the REST poller. |
| `Endpoint` | `string` | Binance REST path or full URL. |
| `Interval` | `time.Duration` | Poll cadence (configured via YAML/env). |
| `Parser` | `string` | Adapter parser label mapped to concrete decode function. |

### `dispatcher.FilterRule`
| Field | Type | Description |
| --- | --- | --- |
| `Field` | `string` | Path expression (dot-notation) pointing into the `RawInstance`. |
| `Op` | `string` | Operator (`eq`, `neq`, `in`, `prefix`). |
| `Value` | `any` | Comparison operand. |

### Control Plane State
- `ControlVersion` increments with each control command to allow idempotent updates.
- Dispatcher emits audit `MelticaEvent` on control changes with `Type=control.update`.

## Control Bus Messages

### `schema.Subscribe`
| Field | Type | Description |
| --- | --- | --- |
| `Type` | `CanonicalType` | Target canonical event type. |
| `Filters` | `map[string]any` | Consumer-specified filter overrides (by instrument, market, etc.). |
| `TraceID` | `string` | Optional control-plane trace for audit spans. |

### `schema.Unsubscribe`
| Field | Type | Description |
| --- | --- | --- |
| `Type` | `CanonicalType` | Canonical event type to remove. |
| `TraceID` | `string` | Optional trace identifier. |

### Control Bus Acknowledgement
- `dispatcher.ControlAck` (internal struct) includes `Type`, `Status` (`accepted`, `noop`, `error`), `DispatchVersion`, and optional error details for invalid requests.

## Snapshot Domain

### `snapshot.Record`
| Field | Type | Description |
| --- | --- | --- |
| `Instrument` | `string` | Canonical symbol for the cached instrument. |
| `Market` | `string` | Market venue identifier. |
| `Type` | `CanonicalType` | Event type represented by the snapshot. |
| `Seq` | `uint64` | Latest applied sequence number. |
| `Version` | `uint64` | Snapshot version for CAS operations. |
| `Data` | `map[string]any` | Canonical payload representing the current state. |
| `UpdatedAt` | `time.Time` | Last mutation timestamp. |
| `TTL` | `time.Duration` | Optional expiry to support cleanup. |

### `snapshot.Store` Interface
```
type Store interface {
    Get(ctx context.Context, key Key) (Record, error)
    Put(ctx context.Context, record Record) error
    CompareAndSwap(ctx context.Context, prevVersion uint64, record Record) (Record, error)
}
```
- `Key` derived from `(Market, Instrument, Type)`.
- Errors use `*errs.E` with codes `snapshot/not-found`, `snapshot/conflict`, `snapshot/unavailable`.
- Default implementation uses a per-key RWLock + version counter to enforce atomicity (EP-06).

### State Transitions
1. **Initial Put**: On first REST snapshot, Conductor calls `Put`; version initialized to `1`.
2. **CAS Update**: For WS delta fusion, Conductor loads current record, applies merge, and calls `CompareAndSwap`. On conflict, it retries with refreshed data.
3. **TTL Expiry**: Background sweeper (future work) removes stale records after configured TTL without breaking atomic guarantees.

## Bus Abstractions

### `databus.Bus`
- Interface exposes `Publish(ctx, MelticaEvent) error` and `Subscribe(ctx, CanonicalType) (<-chan MelticaEvent, error)`.
- Default stub uses bounded buffered channels sized per configuration to enforce backpressure.
- Sequencing: per-instrument worker ensures ordered delivery before fan-out.

### `controlbus.Bus`
- Interface exposes `Send(ctx, Command) error` and `Consume(ctx) (<-chan Command, error)` where `Command` is either `Subscribe` or `Unsubscribe`.
- Stub implementation multiplexes commands over bounded channels with audit logging.

### Relationship Overview
- Binance Adapter emits `schema.RawInstance` → Dispatcher consults `DispatchTable` → canonicalizes into `schema.MelticaEvent` → Conductor orchestrates using `snapshot.Store` and publishes via `databus.Bus` → Control commands flow through `controlbus.Bus` back into Dispatcher.
