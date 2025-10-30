# Domain Layer

The domain layer captures Meltica's canonical data model and shared error
envelopes. Packages here are intentionally free of infrastructure concerns so
that they can be reused across application flows and tests.

- `schema/` defines canonical events, payloads, instruments, and route
  metadata exchanged between providers, the dispatcher, and strategies.
- `errs/` provides structured error codes, canonical categories, and helpers for
  wrapping venue responses.

Domain types should remain side-effect free; any integration logic belongs under
`internal/infra`, and orchestration belongs under `internal/app`.
