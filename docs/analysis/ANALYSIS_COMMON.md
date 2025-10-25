# Meltica: Common Views vs. Production Systems (Prioritized)

This document consolidates the most frequently shared views across ANALYSIS_CODEX.md, ANALYSIS_DROID.md, ANALYSIS_CLAUDE.md, and ANALYSIS_GEMINI.md, prioritized by urgency for production readiness.

## P0 — Critical Blockers
- Risk management and safety controls: pre-trade checks, position/notional limits, circuit breakers/kill switch, and order throttling.
- Persistent state and data backbone: order audit log, positions & real-time PnL tracking, historical tick storage, and crash recovery/checkpointing.
- Backtesting and historical replay: validate strategies before live deployment; simulation with fees, slippage, and latency.
- Real exchange connectivity: authenticated REST/WebSocket adapters, reconnection and rate-limit handling, and symbol normalization.
- Security of control surfaces & secrets: TLS and authn/z for control APIs; proper secrets management (vault/rotation).

## P1 — High Priority
- Robust OMS/execution: order state machine, amend/bulk operations, advanced order types (stop/stop-limit/OCO), and execution analytics (slippage/latency).
- Reliability & scalability: durable messaging (e.g., NATS/Kafka), horizontal scaling, replay/backfill mechanisms.
- Operations & monitoring: CI/CD, IaC/Kubernetes, centralized logging, alerting/SLOs, and runbooks.

## P2 — Medium Priority
- Multi-venue routing & failover: smart order routing across venues, liquidity splitting, venue selection/fallback.
- Portfolio/accounting enhancements: portfolio/position management and fee accounting.
- Expanded testing: integration with real venues, chaos/property-based tests, and performance/latency regression.

## P3 — Lower Priority / Nice-to-Have
- Advanced performance tuning: lock-free structures, CPU pinning/NUMA (HFT-focused).
- ML/optimization features: parameter optimization, walk-forward analysis, and model/feature-store integration.

---

Last updated: 2025-10-25
