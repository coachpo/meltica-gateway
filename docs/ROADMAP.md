# Meltica: Common Views vs. Production Systems (Prioritized)

This document consolidates the most frequently shared views across ANALYSIS_CODEX.md, ANALYSIS_DROID.md, ANALYSIS_CLAUDE.md, and ANALYSIS_GEMINI.md, prioritized by urgency for production readiness.

## Open Items — Urgency & Importance Sorting

### Urgent and important

#### POSTPONED

- Persistent state and data backbone:
  - [Done] Order/exec/balance audit trail persisted via the Postgres stores plus SQL migrations (`orders`, `executions`, `balances`, `events_outbox`).
  - [Done] Provider/strategy snapshots + HTTP context backup/restore give crash recovery and checkpointing.
  - [Pending] Positions with live PnL tracking and historical tick storage still need dedicated services.
- Security of control surfaces & secrets: TLS and authn/z for control APIs; proper secrets management (vault/rotation).

#### PLANNING

- Robust OMS/execution: order state machine, amend/bulk operations, advanced order types (stop/stop-limit/OCO), and execution analytics (slippage/latency).

### Urgent and not important

- None

### Not urgent and important

#### POSTPONED

- Reliability & scalability:
  - [Done] Durable in-process bus wrapped with the Postgres outbox (replay worker + persistence) gives guaranteed delivery/backfill.
  - [Pending] Horizontal scale-out and external brokers (NATS/Kafka) are still open.
- Operations & monitoring:
  - [Done] OTLP telemetry provider + published Grafana dashboards/runbooks cover metrics and alert references.
  - [Pending] CI/CD automation, IaC/Kubernetes manifests, and centralized log pipelines remain outstanding.
- Multi-venue routing & failover: smart order routing across venues, liquidity splitting, venue selection/fallback.

- Expanded testing: integration with real venues, chaos/property-based tests, and performance/latency regression.

#### PLANNING

- Portfolio/accounting enhancements: portfolio/position management and fee accounting.

### Not urgent and not important

#### POSTPONED

- Advanced performance tuning: lock-free structures, CPU pinning/NUMA (HFT-focused).
- ML/optimization features: parameter optimization, walk-forward analysis, and model/feature-store integration.

#### PLANNING

- None

## Completed Items — Urgency & Importance Sorting

### Urgent and important

- [Done] Risk management and safety controls: pre-trade checks, position/notional limits, circuit breakers/kill switch, and order throttling.
- [Done] Real exchange connectivity: authenticated REST/WebSocket adapters, reconnection and rate-limit handling, and symbol normalization.

### Urgent and not important

- None

### Not urgent and important

- [Done] Backtesting and historical replay: validate strategies before live deployment; simulation with fees, slippage, and latency.

### Not urgent and not important

- None

---

Last updated: 2025-11-08
