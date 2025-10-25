# Meltica vs. Production-Grade Auto-Trading Platforms

## Present Capabilities
- **Event ingestion & routing:** In-memory `eventbus` fan-out with pooled `schema.Event` objects and configurable buffer/worker counts; dispatcher maintains deterministic route tables and registrar for lambda subscriptions.
- **Strategy runtime:** Lambda manager supports manifest-driven and HTTP-controlled lifecycle for strategies (delay, logging, momentum, mean reversion, grid, market making) with hot reload/start/stop semantics and pooled object reuse.
- **Provider abstraction:** Registry/factory pattern for adapters with lifecycle management, subscription orchestration, and order submission hooks; ships with synthetic provider and manifests for aliasing future venues.
- **Configuration & telemetry:** Single YAML source of truth for exchanges, pools, event bus, and OTLP telemetry; REST control surface (`internal/lambda/runtime/http.go`) and OpenTelemetry integration plus curated Grafana dashboards.
- **Testing foundations:** Table-driven tests around dispatcher routing, event bus fan-out, and pooling behaviour alongside benchmarks and linting targets in `Makefile`.

## Gaps Versus Production Auto-Trading Systems

### Market Connectivity & Data Quality
- Only a synthetic adapter is bundled; no live exchange connectivity, consolidated market data feeds, FIX/REST/WebSocket handlers, or reconciliation against venue sequence numbers.
- Lacks instrument metadata management (tick sizes, contract specs), corporate action handling, and multi-venue normalization required for cross-market strategies.

### Order Lifecycle & Risk Controls
- No full order-management system: missing state machines for new/ack/fill/cancel events, persistence of open orders, or resynchronization on reconnect.
- Absence of pre-trade risk checks (exposure, credit, price collars), post-trade PnL accounting, and portfolio/position tracking; `pool.OrderRequest` is fire-and-forget.
- No support for multi-account routing, account segregation, or compliance logging/audit trails mandated in regulated markets.

### Reliability & Scalability
- Event bus and dispatcher are single-process, in-memory constructs without durability, HA clustering, or replay/backfill mechanisms; process loss equals total state loss.
- No disaster recovery, hot/cold standby, or health checking; dependency on Go standard logger with no structured logging/alerting pipeline.
- Lacks latency monitoring, load shedding, and deterministic sequencing guarantees under contention beyond goroutine fan-out.

### Tooling, Backtesting, and Operations
- No historical data store or simulation engine for research/backtesting; strategies cannot be validated against historical fills or Monte Carlo stress tests.
- Configuration is static YAML without secret management, environment promotion, or dynamic feature toggles.
- Missing deployment automation (container images, CI/CD, infrastructure as code for production networking) and operational runbooks.
- Security posture is undefined: no key management, authentication/authorization for the REST control API, or audit of strategy changes.

## Improvement Priorities
1. **Integrate real exchange connectors** (market data + trading) with resilient session management, sequencing, and instrument catalogs.
2. **Build a robust OMS layer** covering order state, persistence, and recovery plus pre-/post-trade risk checks and portfolio accounting.
3. **Harden runtime reliability** via durable messaging (e.g., NATS/Kafka), horizontal scaling, health checks, and structured observability with alerting.
4. **Add research & validation tooling** including backtesting harnesses, simulation environments, and automated strategy certification gates.
5. **Secure & operationalize control surfaces** by introducing authn/z, secret management, deployment automation, and compliance-grade audit logging.
