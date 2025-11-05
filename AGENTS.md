# Repository Guidelines

## Project Structure & Module Organization
The gateway entrypoint lives in `cmd/gateway`, wiring pools, the event bus, and the REST control plane. Core packages reside in `internal/` (split into `app/`, `domain/`, `infra/`, and `support/`), while exported contracts live in `api/`. Frontend and operator tooling sit in `web/` and `scripts/`, respectively. Configuration defaults ship in `config/`, deployment assets in `deployments/`, and architectural docs in `docs/`. Tests are split between package-level suites alongside source files and higher-level contract/architecture suites in `test/` and `tests/`.

## Build, Test, and Development Commands
Use `make run` for a fast feedback loop (`go run ./cmd/gateway/main.go`). `make build` emits binaries into `bin/`, and `make build-linux-arm64` cross-builds plus copies YAML configs for packaging. Quality gates: `make lint` executes `golangci-lint` with `.golangci.yml`, `make test` runs `go test ./... -race -count=1 -timeout=30s`, and `make coverage` enforces the ≥70 % TS-01 threshold (view with `go tool cover -html=coverage.out`). Profiling hot paths? `make bench` benchmarks packages, and `make backtest STRATEGY=meanreversion` drives the offline runner.

## Coding Style & Naming Conventions
Format all Go code with `gofmt` (tabs, no spaces) and `goimports`. Respect lint bans: prefer `github.com/goccy/go-json` over `encoding/json` and `github.com/coder/websocket` instead of Gorilla. Keep identifiers idiomatic—PascalCase for exported types, lowerCamelCase for internals, and package names as short nouns (`eventbus`, `telemetry`). Avoid introducing `legacy`, `deprecated`, `shim`, or `feature_flag` identifiers; `forbidigo` will reject them. Document non-obvious flows with concise comments adjacent to the code.

## Testing Guidelines
Table-driven tests with descriptive suffixes (`TestDispatcher_Register/duplicateProvider`) keep output searchable. Co-locate unit tests with their packages and place shared fixtures under `testdata/`. Contract suites in `tests/contract` expect deterministic fixtures; update snapshots before merging. Always run `make test` prior to a pull request, and gate merges with `make coverage` to confirm the 70 % minimum. Prefer focused benchmarks over ad-hoc profiling to protect regression budgets.

## Commit & Pull Request Guidelines
Follow the existing conventional prefixes visible in `git log`: `feat:`, `fix:`, `docs:`, `refactor:`, etc., optionally adding a scope (`feat(dispatcher): ...`). Summaries stay in imperative mood and under ~72 characters. PRs must include: concise description of the change, linked issues or strategy IDs, checklists confirming `make lint` and `make test` passed, and screenshots or logs when UI or telemetry output changes. Keep PRs focused; split large refactors into preparatory commits when possible.

## Configuration & Telemetry Notes
Create local configs via `cp config/app.example.yaml config/app.yaml` and adjust provider aliases, pool sizes, and telemetry endpoints before running the gateway. The control API defaults to `:8880`; document any port tweaks in the PR to keep dashboards aligned. When altering telemetry or metrics, update `docs/dashboards/` and `TELEMETRY_POINTS.md` alongside code so operators can redeploy collectors without guesswork.
