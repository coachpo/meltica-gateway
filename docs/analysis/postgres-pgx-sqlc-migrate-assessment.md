# PostgreSQL + pgx + sqlc + golang-migrate Assessment

## Executive Summary
- PostgreSQL with pgx, sqlc, and golang-migrate already anchors the persistence layer, so continued investment requires incremental hardening instead of a greenfield rollout (`go.mod`:10, `go.mod`:11, `internal/infra/persistence/postgres/order_store.go`:12).
- The stack promises consistent type-safe access, first-class migration control, and predictable runtime characteristics aligned with Meltica’s concurrent trading workloads.
- Remaining work centers on finishing the planned migration cleanup, enforcing regeneration workflows, and expanding operational guardrails documented in the persistence execution plan (`docs/plans/backend/5-persistence-upgrade-execution-plan.md`:5).
- With disciplined tooling ownership and contract testing, the stack supports future expansion without introducing significant architectural risk.

## Current State
- Domain stores wire sqlc-generated query objects over a shared pgx pool, demonstrating a functioning integration path today (`internal/infra/persistence/postgres/store.go`:8, `internal/infra/persistence/postgres/order_store.go`:21).
- Generated sqlc code is tracked in `internal/infra/persistence/postgres/sqlc/`, showing the project already relies on typed bindings instead of ad-hoc SQL (`internal/infra/persistence/postgres/sqlc/orders.sql.go`:1).
- Migrations live under `db/migrations/` with procedural guidance for golang-migrate in developer docs, enabling repeatable schema evolution (`docs/development/migrations.md`:3).
- The persistence upgrade plan lays out phased tasks, indicating partial completion and clarifying remaining backlog items like contract tests and migration hygiene (`docs/plans/backend/5-persistence-upgrade-execution-plan.md`:5).

## Feasibility
- Technical fit is high because pgx pools feed directly into existing repositories and adapters, minimizing refactor risk (`internal/infra/persistence/postgres/order_store.go`:22).
- Operational workflows (Make targets for migrations and sqlc generation) are documented, so teams can standardize around the toolchain with minor process reinforcement (`docs/development/migrations.md`:12).
- Organizational readiness is reinforced by the execution plan, which breaks adoption into manageable phases and clarifies ownership across app, infra, and documentation tracks (`docs/plans/backend/5-persistence-upgrade-execution-plan.md`:22).
- Ecosystem maturity for all four components is strong, reducing vendor risk while keeping the stack close to community best practices for Go/Postgres services.

## Expected Benefits
- pgx delivers low-latency I/O, fine-grained connection management, and observability hooks essential for the trading gateway’s bursty load patterns.
- sqlc enforces compile-time safety and schema drift visibility, shrinking runtime defects from malformed SQL and easing code review.
- golang-migrate provides deterministic schema promotion and rollback, satisfying operational requirements for CI/CD and disaster recovery.
- The integrated toolchain supports clean separation between SQL definitions and domain adapters, enabling clearer scaling strategies (read-replicas, sharding) without rewiring call sites.

## Risks & Mitigations
- Toolchain complexity can overwhelm new contributors; mitigate with automated `make sqlc`/`make migrate` hooks and onboarding guides aligned with existing documentation (`docs/development/migrations.md`:34).
- Drift between migrations and generated code may accumulate; enforce CI checks that fail when `sqlc generate` produces diffs and require paired migration/SQL reviews.
- Long-running transactions or pool exhaustion under pgx could degrade performance; apply observability via the project’s OpenTelemetry integration and define pool budgets per workload.
- Generated code churn may mask logic changes; maintain thin adapters and focus reviews on handwritten files to keep diffs intelligible (`internal/infra/persistence/postgres/order_store.go`:67).

## Recommendations
- Complete Phase 1 of the persistence plan by deleting remaining raw SQL snippets and documenting regeneration steps in the developer runbooks (`docs/plans/backend/5-persistence-upgrade-execution-plan.md`:11).
- Automate contract-level tests that spin up Postgres, apply migrations, and exercise sqlc repositories to catch regressions early (`docs/plans/backend/5-persistence-upgrade-execution-plan.md`:44).
- Wire CI to run `make migrate`/`make test` against a disposable database and ensure migration rollback coverage before merges.
- Establish ownership for `sqlc.yaml` and migration directories, with scheduled reviews to keep schema, docs, and telemetry updates in sync.
