# Application Layer

Packages in this directory coordinate Meltica's runtime behaviour. They wire
domain types from `internal/domain` with infrastructure services from
`internal/infra` to deliver the gateway's core workflows.

- `dispatcher/` maintains routing tables, registrar logic, and the runtime loop
  that fans provider events out to downstream consumers.
- `lambda/` hosts the lambda framework: the base runtime, lifecycle manager, and
  built-in strategy implementations.
- `provider/` defines provider contracts and manages adapter lifecycle,
  including registry and startup sequencing.
- `risk/` enforces runtime risk controls shared across lambda instances.

Application-layer packages should own orchestration only—business state and
canonical types live under `internal/domain`, while side-effecting concerns live
under `internal/infra`.
