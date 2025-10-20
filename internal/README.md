# Internal Packages

This directory contains private application and library code for the Meltica gateway.

## Overview

Code in `/internal` is enforced by the Go compiler to be importable only by code within this project.
This prevents external projects from depending on internal implementation details.

## Structure

### Core Components

- **`adapters/`** - Provider implementations for market data sources
  - `binance/` - Binance exchange adapter
  - `fake/` - Synthetic data generator for testing
  - `shared/` - Shared utilities for adapters

- **`bus/`** - Message bus implementations
  - `eventbus/` - Market data event distribution
  - `controlbus/` - Control plane message routing

- **`consumer/`** - Event consumers and lambda processors
  - Various lambda implementations for different event types

- **`dispatcher/`** - Event routing and dispatch logic
  - Route table management
  - Stream ordering
  - Control plane handlers

- **`schema/`** - Canonical event definitions and schemas
  - Event types and payloads
  - Control message definitions
  - Provider interfaces

- **`pool/`** - Object pooling for high-frequency allocations
  - Event pooling
  - Resource management

- **`config/`** - Configuration loading and management
  - Streaming configuration
  - Runtime configuration

- **`telemetry/`** - OpenTelemetry instrumentation
  - Tracing setup
  - Metrics collection

- **`errs/`** - Structured error handling utilities

## Design Principles

1. **Privacy**: No external imports allowed - keeps internal details private
2. **Separation of Concerns**: Each package has a clear, focused responsibility
3. **Minimal Dependencies**: Packages depend on abstractions, not implementations
4. **In-Memory Only**: No persistence layer - configuration state only
5. **High Performance**: Object pooling and efficient event routing

## Import Guidelines

- Prefer interfaces from `/internal/schema` for cross-package communication
- Avoid circular dependencies
- Keep package APIs minimal and focused
