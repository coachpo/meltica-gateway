# Fake Provider Implementation Checklist

- [ ] Emit ticker, trade, order book, and balance updates automatically after startup.
- [ ] Trigger `orderExec` events and matching balance updates whenever orders exist.
- [ ] Support every event type defined in `internal/schema/event.go` (RiskControl events are emitted by `internal/risk` and are excluded here).
- [ ] Use shared helper functions for publishing each data feed so downstream wiring remains automatic.
- [ ] Merge polled order book snapshots with WebSocket deltas and emit a single normalized order book stream.
- [ ] Structure code for reuse so real exchange adapters can implement the same helper set and inherit expected behavior.
