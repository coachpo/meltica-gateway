# Repository Guidelines

## Project Structure & Module Organization
`cmd/gateway` is the gateway binary, configured via `config/app.yaml` or `MELTICA_CONFIG_PATH`. Core packages live under `internal/app` (composition), `internal/domain` (entities), `internal/infra` (bus, telemetry, persistence), and `internal/support`. Contracts stay in `api/`, while observability and lambda docs live in `docs/` and `deployments/telemetry`. Database migrations live in `db/migrations`, and SQLC output sits in `internal/infra/persistence/postgres/sqlc`. Submodules (`strategies/`, `hypnotism/`) provide reusable strategies and experimental adapters. Tests are colocated with code; cross-package suites use `tests/contract/*` and fixtures live in `test/`.

## Build, Test & Development Commands
- `make run CONFIG_FILE=config/app.yaml` — run the gateway with any config (or set `MELTICA_CONFIG_PATH`).
- `make build` / `make build-linux-arm64` — compile to `bin/`; the ARM64 target also stages configs.
- `make lint` — run `golangci-lint` using `.golangci.yml`.
- `make test` — execute `go test ./... -race -count=1 -timeout=30s`.
- `make coverage` — enforce the TS-01 ≥70% bar and emit `coverage.out` for review.
- `make bench` — measure hot paths before or after perf work.
- `make migrate` / `make migrate-down` — apply or roll back via `cmd/migrate` against `DATABASE_URL`.
- `sqlc generate` — regenerate typed repositories after editing SQL.

## Coding Style & Naming Conventions
Target Go 1.25; run `gofmt`/`goimports` before committing and rely on `golangci-lint` (staticcheck, revive, gocyclo) for extra enforcement. Use tabs, `camelCase` locals, and `PascalCase` exports, and keep package names short (`dispatcher`, `telemetry`, `lambda`). Align config keys and strategy IDs with provider aliases (e.g., `fake:`, `binance:`) to keep dispatcher bindings readable. Prefer constructors inside `internal/app` rather than mutable globals.

## Testing Guidelines
Keep `_test.go` files next to the code they cover, and move multi-package or contract suites into `tests/contract/*` (run `make contract-ws-routing` when touching websocket routing). Every PR must pass `make coverage`; if the ≥70% bar slips, add table-driven cases or cite supporting benchmarks. Name tests `Test<Component>_<Scenario>` so `go test` stays searchable.

## Commit & Pull Request Guidelines
History favors Conventional Commits (`feat(submodules): …`, `chore(strategies): …`), so stick to `type(scope): summary` and list the validation commands you ran (`make lint && make test && make coverage`). Reference tickets, and call out config or migration impacts explicitly. In PR descriptions, mention telemetry/doc touchpoints and attach screenshots or logs whenever UI or observability output changes.

## Security & Configuration Tips
Never commit credentials—use `.env` for `DATABASE_URL`, OTLP endpoints, and exchange keys. Demonstrate config edits in `config/app.example.yaml`, and keep `config/app.ci.yaml` deterministic for CI. Rotate sample secrets immediately if exposure is suspected.
