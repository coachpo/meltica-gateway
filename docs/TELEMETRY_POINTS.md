# Telemetry Points

The control plane emits the following OpenTelemetry (OTLP) metrics. All metrics include the implicit resource attributes configured in `internal/infra/telemetry` (service name, deployment environment, etc.).

## Strategy Runtime

| Metric | Type | Labels | Description |
| ------ | ---- | ------ | ----------- |
| `strategy_revision_instances` | Observable gauge | `environment`, `strategy`, `hash` | Number of running lambda instances per revision. Updated by `Manager.observeRevisionUsage`. |
| `strategy_revision_instances_total` | Counter | `environment`, `strategy`, `hash`, `action` (`start` / `stop`) | Lifecycle transitions for each `{strategy,hash}` tuple. Incremented whenever instances start/stop. |
| `strategy_tag_reassigned_total` | Counter | `environment`, `strategy`, `tag` | Counts Docker-style tag moves (`PUT /strategies/modules/{name}/tags/{tag}` or `reassignTags`). Only increments when the target hash actually changes. |
| `strategy_tag_deleted_total` | Counter | `environment`, `strategy`, `tag`, `allowOrphan` | Counts alias deletions (`DELETE /strategies/modules/{name}/tags/{tag}`). `allowOrphan=true` indicates the operator intentionally removed the final selector for a hash. |
| `strategy.upload.validation_failure_total` | Counter | `environment`, `stage` (`compile`, `validation`, `unknown`, …) | Validation failures returned by the JS loader when uploading modules. One sample per diagnostic entry.

The `scripts/dashboards/` Grafana JSON files use these metrics for tag churn alerts and rollout dashboards. Whenever you add additional telemetry points, append them here with the metric name, type, labels, and a short description.
