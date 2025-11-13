# Meltica Project Context for Gemini

This document provides context for Gemini AI agents to understand and assist with the Meltica project.

## Project Overview

Meltica is a high-performance gateway written in Go (version 1.25) for aggregating financial exchange market data. It is designed for low-latency event processing, featuring deterministic pipelines and support for lightweight trading lambdas.

The architecture emphasizes performance and observability:
- **Zero-copy transports and object pooling** (`internal/infra/pool`) are used to minimize memory allocation churn.
- An **in-memory fan-out event bus** (`internal/infra/bus/eventbus`) routes events efficiently.
- A **lambda runtime** (`internal/app/lambda`) allows for dynamic management of trading strategies via a REST API.
- **OpenTelemetry (OTLP)** is integrated for metrics and tracing (`internal/infra/telemetry`), with pre-built Grafana dashboards available in `docs/dashboards/`.
- The backend database is **PostgreSQL**, with the `sqlc` tool used to generate type-safe Go code for database interactions from raw SQL queries.

## Building and Running

The project uses a `Makefile` to standardize common development tasks.

### Prerequisites

- Go 1.25+
- `golangci-lint`
- Docker (for running PostgreSQL and other services)

### Key Commands

- **Run the gateway:**
  ```bash
  make run
  ```
  This command starts the main gateway application. By default, it uses the configuration from `config/app.yaml`.

- **Build the binary:**
  ```bash
  make build
  ```
  This compiles all packages and places the resulting binaries in the `bin/` directory.

- **Run tests:**
  ```bash
  make test
  ```
  This executes the test suite, including race condition checks.

- **Lint the code:**
  ```bash
  make lint
  ```
  This runs `golangci-lint` using the rules defined in `.golangci.yml` to ensure code quality and consistency.

- **Database Migrations:**
  The project uses `golang-migrate` for managing the database schema.
  ```bash
  # Ensure DATABASE_URL is set in your environment or .env file
  make migrate      # Apply all pending migrations
  make migrate-down # Roll back the last migration
  ```

- **Generate SQL-to-Go code:**
  After modifying SQL queries in `internal/infra/persistence/postgres/sql/`, run:
  ```bash
  sqlc generate
  ```
  This regenerates the Go database access code in `internal/infra/persistence/postgres/sqlc/`.

## Development Conventions

- **Configuration:**
  - The primary configuration file is `config/app.yaml`.
  - For local development, copy `config/app.example.yaml` to `config/app.yaml` and modify it as needed.
  - Environment variables (e.g., `DATABASE_URL`) can be managed in an `.env` file (copy from `.env.example`).

- **Code Style:**
  - Code style is enforced by `golangci-lint`. Refer to `.golangci.yml` for the specific linting rules.
  - The project follows standard Go project layout conventions.

- **Testing:**
  - Unit and integration tests are located in `_test.go` files alongside the code they test.
  - Broader contract and architecture tests are in the `tests/` directory.
  - A minimum of 70% test coverage is enforced by the CI pipeline (`make coverage`).

- **API:**
  - Public API contracts are defined in the `api/` directory.
  - The control plane for managing lambdas is documented in `docs/lambdas-api.md`.
