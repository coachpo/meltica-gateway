# Meltica: Common Views vs. Production Systems (Prioritized)

This document consolidates the most frequently shared views across ANALYSIS_CODEX.md, ANALYSIS_DROID.md, ANALYSIS_CLAUDE.md, and ANALYSIS_GEMINI.md, prioritized by urgency for production readiness.

## Open Items — Urgency & Importance Sorting

### Urgent and important

#### POSTPONED

- Persistent state and data backbone:
  - [Pending] Positions with live PnL tracking and historical tick storage still need dedicated services.
- Security of control surfaces & secrets: TLS and authn/z for control APIs; proper secrets management (vault/rotation).

#### PLANNING

- Robust OMS/execution: order state machine, amend/bulk operations, advanced order types (stop/stop-limit/OCO), and execution analytics (slippage/latency).

### Urgent and not important

- None

### Not urgent and important

#### POSTPONED

- Reliability & scalability:
  - [Pending] Horizontal scale-out and external brokers (NATS/Kafka) are still open.
- Operations & monitoring:
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
- [Done] Core persistence backbone: Postgres-backed order/execution/balance stores plus provider/strategy snapshots with HTTP context backup/restore.

### Urgent and not important

- None

### Not urgent and important

- [Done] Backtesting and historical replay: validate strategies before live deployment; simulation with fees, slippage, and latency.
- [Done] Durable in-process bus with Postgres outbox replay for guaranteed delivery/backfill.
- [Done] Telemetry & dashboards: OTLP metric exporter, published Grafana packs, and accompanying runbooks for operators.

### Not urgent and not important

- None

## Proposed Upgrades

Based on the analysis, here are a few potential upgrades that could enhance the Meltica platform:

1.  **Web-based User Interface:** While the REST API is powerful for developers, a web-based UI would make the platform more accessible. The UI could provide:
    *   A dashboard for monitoring the status of providers, lambdas, and orders.
    *   A code editor for creating and editing lambdas directly in the browser.
    *   A form-based interface for configuring providers.

3.  **Support for More Languages in Lambdas:** Currently, it seems that lambdas are written in JavaScript (via `goja`). Adding support for other popular languages for quantitative finance, such as Python, would broaden the appeal of the platform. This could be achieved by integrating a Python interpreter or by using a plugin-based architecture.

4.  **Hot-Reloading of Configuration:** The application currently loads its configuration at startup. Implementing a mechanism to hot-reload the configuration without restarting the gateway would improve its usability and reduce downtime.

5.  **Market Replay Functionality:** A feature to "replay" historical market data through the gateway would be invaluable for debugging and testing trading strategies. This would allow developers to test their lambdas against specific historical scenarios.


---

Last updated: 2025-11-08
