# Infrastructure Layer

Infrastructure packages encapsulate integrations with external systems and
cross-cutting platform capabilities used by the Meltica gateway.

- `adapters/` contains the first-party exchange adapters (e.g. Binance) and
  shared adapter utilities.
- `bus/` implements the in-memory event bus that fans canonical events to
  dispatcher subscribers.
- `config/` loads and validates the YAML application configuration into typed
  structures.
- `pool/` manages pooled allocations for hot-path event objects.
- `server/` exposes the HTTP control-plane handler for managing lambda
  instances.
- `telemetry/` wires OpenTelemetry exporters and semantic conventions.

Code in `internal/infra` should focus on I/O, resource management, and
instrumentation. Business rules and orchestration should remain in
`internal/app`, and canonical types should live in `internal/domain`.
