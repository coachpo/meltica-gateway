# Meltica Minimal Running Scope

## Objective
Provide the smallest feature set required to run Meltica end-to-end for evaluation and demos without production-grade hardening.

## Must-Have Components
- **Provider connectivity:** Implement a single authenticated exchange adapter with basic REST/WebSocket ingestion and symbol normalization.
- **Event flow wiring:** Ensure providers feed the in-memory bus, dispatcher tables route events, and at least one lambda strategy consumes and responds.
- **Core state management:** Maintain transient order/position state in memory and verify pool lifecycle (borrow, process, return) to avoid leaks.
- **Operational scripts:** Supply minimal Makefile targets for build, run, and smoke-test execution.

## Deferrable Components
- **Persistent data backbone:** Audit logs, checkpointing, real-time PnL, and historical tick storage can wait until after the runnable milestone.
- **Advanced OMS features:** Order state machine, amendments, bulk operations, advanced order types, and execution analytics remain out of scope.
- **Security hardening:** TLS termination, authn/z, and secret rotation are postponed for the initial demo environment.
- **Scalability and resilience:** Durable messaging, horizontal scaling, and replay/backfill pipelines are scheduled once core flows stabilize.

## Next Steps After Run Readiness
Once the minimal scope is stable, incrementally add persistence, OMS depth, and security controls, followed by observability, deployment automation, and multi-venue routing.
