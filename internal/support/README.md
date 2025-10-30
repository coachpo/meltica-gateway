# Support Packages

Support packages provide tooling and utilities that sit outside the core
runtime. They are safe to depend on from tests, contract harnesses, or CLI
tools, but should not introduce dependencies back into the application layer.

- `backtest/` implements the historical backtesting engine plus feeders,
  simulated exchange, and analysis helpers.

Where possible these helpers should depend on domain types (`internal/domain`)
instead of application orchestration packages to keep feedback loops short.
