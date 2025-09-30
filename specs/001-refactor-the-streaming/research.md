# Research: Dispatcher-Conductor Streaming Refactor

## Observability Export Strategy

- **Decision**: Instrument the pipeline with the Go OpenTelemetry SDK, defaulting to the OTLP/HTTP exporter configured via YAML/env, while falling back to a no-op provider when telemetry is disabled.
- **Rationale**: OTLP/HTTP keeps dependencies lightweight, matches collector defaults, and supports both traces and metrics across Adapter → Dispatcher → Conductor without forcing a specific backend.
- **Alternatives considered**: OTLP/gRPC (heavier dependency surface), stdout exporters (good for debugging but not production ready), bespoke logging (lacks cross-service correlation).

## Snapshot Store Backend

- **Decision**: Define a `SnapshotStore` interface that supports `Get`, `Put`, and `CompareAndSwap` semantics with a default in-memory implementation backed by per-instrument locks and version counters; expose hooks for plug-in adapters (e.g., Redis) via the same contract.
- **Rationale**: In-memory store satisfies orchestration atomicity requirements for initial scope, while the interface keeps future persistence options open without leaking vendor specifics.
- **Alternatives considered**: Direct map without CAS (fails EP-06 atomicity), database-backed store first (slows delivery and adds migrations), embedding snapshot logic in Conductor (blurs responsibilities).

## Bus Abstraction Scope

- **Decision**: Provide `databus` and `controlbus` interfaces that operate over bounded channels with pluggable backends, supplying an in-memory fan-out implementation for smoke tests and leaving NATS/Kafka/Redis adapters to future work.
- **Rationale**: Bounded channels satisfy backpressure requirements while allowing deterministic testing; the interface layer keeps protocol-specific code outside core orchestration.
- **Alternatives considered**: Hard-coding a single message broker (limits deploy flexibility), unbounded channels (risk memory growth), deferring abstraction until later (violates spec’s stub requirement).
