# Contract Test Harness

The contract suites under `tests/contract/` spin up disposable infrastructure (currently PostgreSQL) to exercise Meltica’s persistence adapters end to end. They complement the unit tests that run purely in-memory by validating migrations, sqlc bindings, and domain stores against a real database.

## Prerequisites

- **Docker Engine** (desktop or daemon). Testcontainers manages the lifecycle; ensure your user can talk to the Docker socket.
- Go toolchain 1.25+ to run the tests.
- Optional but recommended: `sqlc` and `golang-migrate` so you can regenerate bindings and migrations before running the suite (CI enforces this).

## Running the Tests

```bash
# Fast path while developing persistence changes
go test ./tests/contract/persistence -count=1 -race

# Or run the entire module (requires Docker because the contract suite is included)
make test
```

The harness will:

1. Start a `postgres:16-alpine` container.
2. Apply every migration under `db/migrations/` via `golang-migrate`.
3. Allocate a `pgxpool.Pool` and exercise provider, strategy, order, balance, and outbox stores through the real database.

If Docker isn’t available the suite skips automatically, but CI always provides the service, so local parity is encouraged.

## Troubleshooting

- **Container startup stalls**: ensure Docker Desktop (or the daemon) is running and your user is in the right group. The tests will wait up to ~60s before failing.
- **Leftover containers**: Testcontainers uses the Ryuk sidecar for cleanup. In restricted environments you can set `TESTCONTAINERS_RYUK_DISABLED=true` and clean up manually.
- **Custom daemon host**: Export `TESTCONTAINERS_HOST_OVERRIDE` or `DOCKER_HOST` if you route through TCP sockets or remote daemons.
- **Permission errors with mounted sockets**: On Linux, add your user to the `docker` group or run the tests via `sudo -E env ... go test ...` (last resort).

## When to Run

- Any time you edit migrations (`db/migrations/`) or SQL files consumed by `sqlc`.
- When touching `internal/infra/persistence/postgres/*` to confirm behavior matches the schema.
- Before sending a PR that affects the persistence plan so reviewers can rely on real DB coverage.

For CI, GitHub Actions provisions Postgres as a service and runs `go test ./...`, so no additional wiring is necessary beyond ensuring Docker is enabled on the runner (already handled in `.github/workflows/ci.yml`).
