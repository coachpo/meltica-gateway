# Meltica Gemini Agent Context

This document provides context for the Gemini agent to understand and interact with the Meltica codebase.

## Project Overview

Meltica is a high-performance cryptocurrency exchange adapter framework written in Go. Its purpose is to provide a unified API for interacting with multiple cryptocurrency exchanges, starting with Binance. The framework is designed for low-latency trading and features a strongly-typed, extensible architecture.

Key technologies include:
- **Go:** The primary programming language.
- **WebSocket:** `github.com/coder/websocket` for real-time data streams.
- **JSON Serialization:** `github.com/goccy/go-json` for efficient JSON handling.
- **Observability:** OpenTelemetry for metrics and traces.

The architecture is a formal four-layer system:
1.  **Connection:** Manages WebSocket and REST transports.
2.  **Routing:** Normalizes payloads and manages subscriptions.
3.  **Business:** Coordinates domain workflows and state.
4.  **Filter:** Transforms events for downstream clients.

## Building and Running

The project uses a `Makefile` for common development tasks.

- **Build the project:**
  ```bash
  make build
  ```

- **Run the main application:**
  ```bash
  make run
  ```

- **Run tests:**
  ```bash
  make test
  ```

- **Run linter:**
  ```bash
  make lint
  ```

- **Check test coverage:**
  ```bash
  make coverage
  ```

## Development Conventions

- **Dependency Management:** The project uses Go modules. Dependencies are defined in `go.mod` and `go.sum`.
- **Linting:** Code quality is enforced using `golangci-lint` with the configuration in `.golangci.yml`. Key linting rules include `wrapcheck`, `revive`, `nilerr`, and `gosec`.
- **Banned Libraries:** The project explicitly forbids the use of the standard `encoding/json` and `github.com/gorilla/websocket` packages, favoring `github.com/goccy/go-json` and `github.com/coder/websocket` respectively.
- **Testing:** Tests are written with the standard Go testing library. The project enforces a minimum of 70% test coverage.
- **Code Style:** The `.golangci.yml` file and existing source code should be referenced for code style conventions.
