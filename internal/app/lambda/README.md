# Lambda Layer

Packages under `internal/app/lambda` coordinate Meltica's strategy execution
capabilities:

- `core/` exposes the reusable trading lambda primitives (`BaseLambda`,
  configuration helpers, and shared interfaces).
- `runtime/` manages lambda lifecycle: manifest hydration, dynamic creation, and
  control-plane operations.
- `strategies/` hosts built-in trading strategy implementations along with
  documentation for building custom strategies.

Keep orchestration logic in `runtime`, reusable building blocks in `core`, and
strategy-specific behaviour in `strategies`.
