# PostgreSQL + pgx + sqlc + golang-migrate 再评估

## 【发现】
### 1. 项目结构与依赖变化
- 持续以 `pgx`、`golang-migrate` 作为核心依赖，集中在 `go.mod` 并锁定具体版本，符合统一化要求（`go.mod`:10-11）。
- `sqlc.yaml` 将 schema、查询及代码输出路径固定在 `internal/infra/persistence/postgres/`，同时声明 `pgx/v5`、数值字段覆盖配置，整体结构清晰（`sqlc.yaml`:1-30）。
- 入口 `cmd/gateway/main.go` 在启动前执行迁移、初始化 pgxpool，并将各 Store 装配进应用层，保证运行期一致性（`cmd/gateway/main.go`:69-111）。
- PostgreSQL Store 聚合仍集中在 `internal/infra/persistence/postgres/store.go`，维持薄封装以复用公共连接池（`internal/infra/persistence/postgres/store.go`:8-16）。

### 2. 生成代码质量
- 最新的 sqlc 产物在 `ListOrders`/`ListExecutions` 查询上将 `price_text`、`fee_text` 推断为 `interface{}`，与业务层直接使用 `strings.TrimSpace` 的实现不兼容，`go build` 直接失败（`internal/infra/persistence/postgres/sqlc/orders.sql.go`:198, `internal/infra/persistence/postgres/sqlc/executions.sql.go`:139, `internal/infra/persistence/postgres/order_store.go`:349, `internal/infra/persistence/postgres/order_store.go`:412）。
- SQL 使用 `sqlc.narg`、`sqlc.arg` 表示可选参数，命名与表结构保持一致，整体规范（`internal/infra/persistence/postgres/sql/orders.sql`:1-92）。
- 数值字段统一映射到 `pgtype.Numeric`，再经由仓储层手动 `Scan` / `String` 转换，虽然保证精度，但在读取路径上重复编码（`internal/infra/persistence/postgres/order_store.go`:602-626）。

### 3. 迁移机制
- 迁移入口 `migrations.Apply` 会解析目录、建立 `golang-migrate` 驱动并打点埋点，具备基本的容错与监控（`internal/infra/persistence/migrations/migrate.go`:35-163）。
- Gateway 启动时首要执行迁移，确保实例级别 schema 同步（`cmd/gateway/main.go`:69-81）。
- CI 流程安装 `migrate/sqlc`，运行 `make migrate && make migrate-down`，并校验生成代码最新状态，提供有效保障（`.github/workflows/ci.yml`:35-90）。
- Schema 仍停留在 `0001_init` 与 `0002_strategy_instance_identifiers`，暂未看到后续增量迁移，演进节奏需要关注（`db/migrations/0001_init.up.sql`:1-120, `db/migrations/0002_strategy_instance_identifiers.up.sql`:1-20）。

### 4. 代码风格与可维护性
- SQL 查询命名遵循 `动词+实体`，配合 `-- name:` 约定，可读性良好（`internal/infra/persistence/postgres/sql/orders.sql`:1-92, `internal/infra/persistence/postgres/sql/balances.sql`:1-60）。
- 业务适配层保持“薄包装”策略，集中在 `order_store.go` 等文件中解包查询结果，不过 `List*` 方法中存在大量字符串修剪、类型断言逻辑，增加维护成本（`internal/infra/persistence/postgres/order_store.go`:331-366, `internal/infra/persistence/postgres/order_store.go`:392-419）。
- 单元测试主要验证空连接池的错误路径，尚未覆盖实际数据库交互，难以及时暴露回归（`internal/infra/persistence/postgres/order_store_test.go`:10-41）。

### 5. 开发体验与协作流程
- Makefile 暴露 `migrate`、`migrate-down`、`sqlc` 等常用命令，降低入门门槛（`Makefile`:1-35）。
- `docs/development/migrations.md` 补充本地/CI 操作指南、回退步骤及 Telemetry 关注点，文档维持同步（`docs/development/migrations.md`:12-90）。
- 提供 `scripts/db/dev_reset.sh` 方便本地重置数据库，协作体验友好（`scripts/db/dev_reset.sh`:1-27）。
- CI 强制执行 `sqlc generate`、迁移往返与覆盖率检查，基本满足合并前检查需求（`.github/workflows/ci.yml`:35-140）。

### 6. 风险与优化项
- 生成代码与手写仓储之间的类型不匹配已经阻断编译流程，必须优先修复（`internal/infra/persistence/postgres/sqlc/orders.sql.go`:198, `internal/infra/persistence/postgres/order_store.go`:349）。
- 读路径依赖字符串再解析数值，增加 CPU 开销并放大人为失误风险，建议引入统一的高精度数值类型适配层（`internal/infra/persistence/postgres/order_store.go`:331-350, `internal/infra/persistence/postgres/order_store.go`:397-417, `internal/infra/persistence/postgres/order_store.go`:602-626）。
- Persist 层缺少对真实 Postgres 的集成测试，现有 `_test.go` 难以覆盖 SQL/迁移回归，需尽快补足（`internal/infra/persistence/postgres/order_store_test.go`:10-41, `.github/workflows/ci.yml`:79-113）。

### 7. 整体结论与改进路线图
- 技术栈仍符合 Meltica 的并发与可观测性诉求，结构化配置/文档/CI 保障较为完善。
- 但最新生成代码暴露出类型推断缺口，编译无法通过，表明 `sqlc` 查询定义需要进一步规范；同时，数值与事务处理流程仍缺乏端到端验证。
- 下一阶段应聚焦修复编译阻塞、提升 sqlc 查询约束力、补充集成测试与数值抽象，以恢复生产可用性并减少后续维护成本。

## 【对比】
- 相比上一轮评估报告（`docs/analysis/postgres-pgx-sqlc-migrate-assessment.md`:1-37），CI 现已覆盖 `sqlc` 再生成与迁移往返校验，部分落实了当时“标准化工具链”建议（`.github/workflows/ci.yml`:35-90）。
- 先前报告未暴露的 `interface{}` 类型问题在本次调研中首次出现，说明近期 SQL 改动（例如 `COALESCE(o.price::text, '')`）缺乏生成后验证，引入新的阻断性缺陷（`internal/infra/persistence/postgres/sqlc/orders.sql.go`:198, `internal/infra/persistence/postgres/order_store.go`:349）。
- 过去建议补充集成测试、加强数值映射与迁移节奏，本次代码库仍未落地，风险维持原状（`internal/infra/persistence/postgres/order_store_test.go`:10-41, `db/migrations/0001_init.up.sql`:1-120, `internal/infra/persistence/postgres/order_store.go`:602-626）。

## 【建议】
1. **修复编译阻塞**：在 SQL 中显式转换为 `TEXT`（如 `COALESCE(o.price::text, ''::text)`、`COALESCE(e.fee::text, ''::text)`），或在 sqlc 覆盖中为派生列指定 `pgtype.Text`，确保生成代码与仓储实现兼容（`internal/infra/persistence/postgres/sql/orders.sql`:60-92, `internal/infra/persistence/postgres/sql/executions.sql`:1-80）。
2. **补充集成测试**：使用 Testcontainers/CI Postgres 服务驱动 CRUD 流程，验证 `OrderStore`、`ProviderStore` 等真实行为，替换目前仅验证 nil pool 的样例（`internal/infra/persistence/postgres/order_store_test.go`:10-41, `.github/workflows/ci.yml`:52-90）。
3. **统一数值抽象**：在仓储层引入 `decimal`/自定义数值对象，集中处理 `pgtype.Numeric` ↔ 业务字符串，降低重复转换与精度隐患（`internal/infra/persistence/postgres/order_store.go`:602-626, `internal/infra/persistence/postgres/order_store.go`:331-350）。
4. **强化生成校验**：在 CI 中追加 `go build ./...` 或 `make build` 于 `sqlc` 校验步骤之后，及时捕捉生成代码与业务层的类型错配（`.github/workflows/ci.yml`:40-90）。
5. **规划后续迁移**：根据领域蓝图补齐 providers/routes/orders 之外的演进脚本，并对 `db/migrations/` 目录实施审查流程，避免长时间无增量更新（`db/migrations/0001_init.up.sql`:1-120, `docs/plans/backend/5-persistence-upgrade-execution-plan.md`:5-36）。
