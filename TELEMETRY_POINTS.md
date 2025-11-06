# Telemetry Points

Canonical list of Meltica metrics exported through `internal/infra/telemetry`. All series share common labels: `environment`, `provider`, and `event_type` (where applicable). Dashboards under `docs/dashboards/` and the OTLP/Prometheus deployment guides reference these names.

## Event Bus

| Metric | Type | Description |
| --- | --- | --- |
| `meltica_eventbus_events_published` | Counter | Total events published per provider/event type. |
| `meltica_eventbus_publish_duration_bucket` | Histogram | Publish latency (seconds) per event type. |
| `meltica_eventbus_fanout_size_bucket` | Histogram | Fanout size (subscriber count) per event type. |
| `meltica_eventbus_subscribers` | Gauge | Active subscriber count. |
| `meltica_eventbus_delivery_errors` | Counter | Delivery failures when dispatching to subscribers. |

## Dispatcher

| Metric | Type | Description |
| --- | --- | --- |
| `meltica_dispatcher_events_ingested` | Counter | Events accepted by the dispatcher. |
| `meltica_dispatcher_events_duplicate` | Counter | Deduplicated events. |
| `meltica_dispatcher_processing_duration_bucket` | Histogram | Dispatcher routing latency. |
| `meltica_dispatcher_routing_version` | Gauge | Current routing config version per provider. |

## Resource Pools

| Metric | Type | Description |
| --- | --- | --- |
| `meltica_pool_capacity` | Gauge | Total objects in each pool. |
| `meltica_pool_available` | Gauge | Idle objects ready to borrow. |
| `meltica_pool_objects_active` | Gauge | Checked-out objects. |
| `meltica_pool_objects_borrowed` | Gauge | Borrow operations in flight. |
| `meltica_pool_borrow_duration_bucket` | Histogram | Borrow latency. |

## Clients

| Metric | Type | Description |
| --- | --- | --- |
| `meltica_wsclient_frames_processed` | Counter | WebSocket frames processed per message type. |
| `meltica_wsclient_frame_processing_duration_bucket` | Histogram | Frame processing latency. |
| `meltica_restclient_polls` | Counter | REST poll invocations. |
| `meltica_restclient_poll_duration_bucket` | Histogram | REST poll latency. |
| `meltica_restclient_snapshots_fetched` | Counter | REST snapshots fetched successfully. |

## Orderbook

| Metric | Type | Description |
| --- | --- | --- |
| `meltica_orderbook_buffer_size` | Gauge | Buffer size per symbol. |
| `meltica_orderbook_gap_detected` | Counter | Sequence gaps encountered. |
| `meltica_orderbook_snapshot_applied` | Counter | Snapshots applied to live books. |
| `meltica_orderbook_coldstart_duration_bucket` | Histogram | Coldstart duration for new symbols. |

## Provider-Specific (Binance Example)

Adapters may define venue-specific instruments:

| Metric | Type | Description |
| --- | --- | --- |
| `meltica_provider_binance_orders_received` | Counter | Orders routed to Binance adapter. |
| `meltica_provider_binance_orders_rejected` | Counter | Venue rejections. |
| `meltica_provider_binance_order_latency` | Histogram | Round-trip order latency. |
| `meltica_provider_binance_events_emitted` | Counter | Market data events emitted. |
| `meltica_provider_binance_balance_updates` | Counter | Balance updates received. |
| `meltica_provider_binance_balance_total` / `_available` | Gauge | Latest balance snapshot per asset. |

## Database & Persistence

| Metric | Type | Description |
| --- | --- | --- |
| `meltica_db_pool_connections_total` | Gauge | Total connections managed by the pgx pool. |
| `meltica_db_pool_connections_idle` | Gauge | Idle connections ready for checkout. |
| `meltica_db_pool_connections_acquired` | Gauge | Connections currently checked out. |
| `meltica_db_pool_connections_constructing` | Gauge | Connections being established. |
| `meltica_db_migrations_total` | Counter | Migrations executed via golang-migrate (result attribute: `applied`, `noop`, `failed`). |

## Provider Cache

| Metric | Type | Description |
| --- | --- | --- |
| `meltica_provider_cache_hits` | Counter | Cache hits for provider metadata/detail lookups. |
| `meltica_provider_cache_misses` | Counter | Cache misses for provider metadata/detail lookups. |

Write-through cache behavior is documented in `docs/development/write-through-cache.md`.

## Operational Notes

- Dashboards listed in `docs/dashboards/README.md` only use the metrics above to avoid drift.
- When adding or renaming a metric, update this file, the Grafana JSON exports, and any Prometheus alert rules in `deployments/telemetry/`.
