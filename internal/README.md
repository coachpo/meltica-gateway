# Internal Packages

This directory contains private application and library code for the Meltica gateway.

## Overview

Code in `/internal` is enforced by the Go compiler to be importable only by code within this project.
This prevents external projects from depending on internal implementation details.

## Structure

- **`app/`** – Application runtime orchestration
  - `dispatcher/` – Routing tables, registrar, and runtime fan-out
  - `lambda/` – Lambda primitives (`core/`), lifecycle (`runtime/`), and built-in strategies (`strategies/`)
  - `provider/` – Registry, manager, and provider contracts
  - `risk/` – Runtime risk-limit enforcement

- **`domain/`** – Core canonical types and shared error envelopes
  - `schema/` – Canonical event structures, payloads, and route metadata
  - `errs/` – Structured error codes and exchange error helpers

- **`infra/`** – Infrastructure and platform integrations
  - `adapters/` – Built-in exchange adapters plus shared utilities
  - `bus/` – In-memory event bus implementation
  - `config/` – Typed configuration loader and helpers
  - `pool/` – Object pool manager and helpers
  - `server/` – HTTP control-plane surface
  - `telemetry/` – OpenTelemetry wiring and semantic conventions

- **`support/`** – Tooling and offline utilities
  - `backtest/` – Historical backtesting engine and fixtures

## Design Principles

1. **Privacy**: No external imports allowed - keeps internal details private
2. **Separation of Concerns**: Each package has a clear, focused responsibility
3. **Minimal Dependencies**: Packages depend on abstractions, not implementations
4. **In-Memory Only**: No persistence layer - configuration state only
5. **High Performance**: Object pooling and efficient event routing

## Import Guidelines

- Prefer interfaces from `/internal/domain/schema` for cross-package communication
- Avoid circular dependencies
- Keep package APIs minimal and focused
