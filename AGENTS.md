# Repository Guidelines

## Project Structure & Module Organization
- Core exchange adapters, pools, and schedulers live under `internal/` (see `internal/provider`, `internal/pool`, `internal/telemetry`).
- Public APIs and shared types live in `api/`, while CLI entrypoints and daemons reside in `cmd/` (notably `cmd/gateway`).
- Reusable scripts sit in `scripts/`, deployment manifests in `deployments/`, and configuration defaults in `config/`. Documentation and dashboards live in `docs/` and `docs/dashboards/`.
- Tests are split between `tests/architecture`, `tests/contract`, `test/`, and in-package `_test.go` files; fixtures should live beside the code inside `testdata/` directories.

## Build, Test, and Development Commands
- `make build` compiles all Go packages into `bin/` for local validation.
- `make run` (or `go run ./cmd/gateway`) boots the reference gateway using `config/app.yaml`.
- `make test` runs `go test ./... -race -count=1 -timeout=30s` and is required before pushing.
- `make coverage` enforces the ≥70% TS-01 threshold and writes `coverage.out`; inspect gaps via `go tool cover -html=coverage.out`.
- `make contract-ws-routing` targets the WebSocket contract harness in `tests/contract/ws-routing` for quick regressions.

## Coding Style & Naming Conventions
- Use idiomatic Go: tabs, `gofmt`/`goimports`, and short doc comments on exported symbols. Keep side effects out of pure helpers and prefer dependency injection for transports.
- File names stay lowercase with underscores only for testdata helpers. Public identifiers use PascalCase; internals prefer lowerCamelCase and remain under `internal/` to signal limited scope.
- `golangci-lint run --config .golangci.yml` plus `go vet ./...` are the authoritative static checks—run them when editing routing, registries, or serialization code.

## Testing Guidelines
- Favor table-driven tests with descriptive suffixes like `TestRouteRegistry_Register/duplicateProvider` so failures map to scenarios.
- Keep fixtures deterministic in `testdata/` and rely on mocks from `tests/contract/ws-routing/mocks` rather than live exchanges.
- Architecture tests in `tests/architecture` guard layering rules; update them whenever cross-layer dependencies change.

## Commit & Pull Request Guidelines
- Recent history shows concise summaries in sentence case (e.g., "Propagate canonical route types"). Start with an imperative verb and focus on observable behavior.
- Each PR should describe impact, list validation commands (`make test`, `make coverage`, etc.), and link issues or specs. Attach screenshots/logs when changing dashboards or telemetry definitions so reviewers can confirm metrics quickly.
