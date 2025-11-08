# Meltica Grafana Dashboards

Optimized dashboards for monitoring Meltica's event streaming system.

## 🎯 Core Dashboards (Start Here)

### meltica-overview-optimized.json
**Your main dashboard** - Essential system health metrics at a glance:
- Event throughput and processing latency
- Pool utilization
- Active subscribers
- Key operational metrics

**Recommended for:** Daily monitoring, incident triage, operations overview

---

### meltica-eventbus-optimized.json
Event bus performance monitoring:
- Event publish rate by type and provider
- Subscriber counts
- Publish latency percentiles (p50, p95, p99)
- Fanout size distribution

**Recommended for:** Event bus performance tuning, subscription management

---

### meltica-dispatcher-optimized.json
Event dispatcher deep-dive:
- Event ingestion rate
- Processing latency percentiles
- Duplicate event detection
- Per-symbol ingestion rates

**Recommended for:** Event processing optimization, duplicate investigation

---

### meltica-pools-optimized.json
Resource pool management:
- Pool utilization percentage
- Borrow latency
- Active vs available objects
- Per-pool metrics

**Recommended for:** Resource capacity planning, connection pool tuning

---

### meltica-database-health.json
PostgreSQL and cache observability:
- Connection totals/idle/acquired/constructing (`meltica_db_pool_connections_*`)
- Migration run counts (`meltica_db_migrations_total`)
- Provider metadata cache hit/miss counters

**Recommended for:** Persistence troubleshooting, verifying migrations and cache behaviour.

---

## 🔍 Specialized Dashboards

### meltica-orderbook-assembler.json
Orderbook assembly and sequencing:
- Buffer sizes by symbol
- Sequence gap detection
- Snapshot application
- Coldstart performance

**Recommended for:** Market data quality monitoring, orderbook debugging

---

### meltica-rest-client.json
REST polling client metrics:
- Poll rate and duration
- Snapshot fetch performance
- HTTP request latency

**Recommended for:** REST API performance, rate limit monitoring

---

### meltica-websocket-client.json
WebSocket frame processing:
- Frame processing rate by message type
- Frame processing latency
- Connection health

**Recommended for:** WebSocket connectivity debugging, message parsing performance

---

### OKX-Provider-Overview.json
Provider-specific lens for OKX adapters:
- Eventbus throughput filtered to provider
- Extension event rate by symbol and payload sizing
- Publish latency and fanout percentiles for OKX traffic

**Recommended for:** Monitoring OKX custom payload adoption and ensuring bus delivery health

---

## 🚀 Quick Start

1. **Import dashboards** into Grafana:
   - Go to Grafana → Dashboards → Import
   - Upload JSON files or paste contents
   - Select your Prometheus datasource

2. **Start with the Overview dashboard** to understand overall system health

3. **Drill down** into specialized dashboards when investigating specific issues

## 📊 Dashboard Design Principles

All optimized dashboards follow these principles:

- ✅ **Only existing metrics** - All queries verified against running Prometheus
- ✅ **Fast queries** - All queries tested for sub-20ms response time
- ✅ **Essential panels only** - Focused on actionable metrics
- ✅ **Template variables** - Filter by environment, provider, event type
- ✅ **Proper time ranges** - 30-minute default with configurable refresh

## 🔧 Query Performance

All dashboard queries are optimized for performance:

- Rate calculations use 5m windows for smoothing
- Histogram quantiles use appropriate bucket sizes
- No expensive regex or nested queries
- Proper use of `clamp_min()` to avoid division by zero

## 📝 Recent Changes

### 2025-11-08
- ✅ Added **Extension Events** row to `Meltica-Overview.json` showing per-provider throughput and average payload size using `meltica_adapter_extension_events` and `meltica_adapter_extension_payload_bytes_*`.
- ✅ Added dedicated **Binance extension panels** to highlight per-symbol adoption and payload sizes on `Binance-Provider-Overview.json`.
- ✅ Created `OKX-Provider-Overview.json` with OKX-specific event throughput, extension payload, and event bus latency panels.

### 2025-10-19

### Removed Dashboards (Broken Metrics)
- ❌ `meltica-alerting-sli.json` - Referenced non-existent error metrics
- ❌ `meltica-overview-bigpicture.json` - Too many panels (21), replaced by optimized version
- ❌ `meltica-dispatcher-runtime.json` - Referenced non-existent dropped events metric
- ❌ `meltica-eventbus-overview.json` - Replaced by optimized version
- ❌ `meltica-resource-utilization.json` - Referenced non-existent delivery blocked metric
- ❌ `meltica-provider-health.json` - Queries used wrong label names
- ❌ `meltica-latency-backpressure.json` - Minimal value, consolidated into other dashboards
- ❌ `meltica-event-type-deep-dive.json` - Redundant with dispatcher dashboard
- ❌ `meltica-pool-ops.json` - Superseded by pools-optimized dashboard
- ❌ `meltica-controlbus.json` - Referenced non-existent send_duration metrics

### Optimized Dashboards
- ✅ Created `meltica-overview-optimized.json` - Essential metrics only (9 panels vs 21)
- ✅ Created `meltica-eventbus-optimized.json` - Focused event bus monitoring
- ✅ Created `meltica-dispatcher-optimized.json` - Streamlined dispatcher metrics
- ✅ Created `meltica-pools-optimized.json` - Resource pool management

### Kept Specialized Dashboards
- ✅ `meltica-orderbook-assembler.json` - Unique orderbook metrics
- ✅ `meltica-rest-client.json` - REST-specific monitoring
- ✅ `meltica-websocket-client.json` - WebSocket-specific monitoring

## 🎯 Available Metrics

All dashboards use these verified metrics from `http://capy.lan:8889/metrics`:

### Event Bus
- `meltica_eventbus_events_published` - Events published counter
- `meltica_eventbus_subscribers` - Active subscriber count
- `meltica_eventbus_publish_duration_bucket` - Publish latency histogram
- `meltica_eventbus_fanout_size_bucket` - Fanout size histogram

### Dispatcher
- `meltica_dispatcher_events_ingested` - Events ingested counter
- `meltica_dispatcher_events_duplicate` - Duplicate events counter
- `meltica_dispatcher_processing_duration_bucket` - Processing latency histogram
- `meltica_dispatcher_routing_version` - Current routing version

### Pools
- `meltica_pool_objects_active` - Active pool objects
- `meltica_pool_capacity` - Pool capacity
- `meltica_pool_available` - Available pool objects
- `meltica_pool_objects_borrowed` - Currently borrowed objects
- `meltica_pool_borrow_duration_bucket` - Borrow latency histogram

### Database & Persistence
- `meltica_db_pool_connections_total` - Total pgx connections (idle + acquired + constructing)
- `meltica_db_pool_connections_idle` - Idle connections ready for checkout
- `meltica_db_pool_connections_acquired` - Connections currently in use
- `meltica_db_pool_connections_constructing` - Connections being established
- `meltica_db_migrations_total` - Migrations executed (attr `result`: applied/noop/failed)
- `meltica_provider_cache_hits` / `meltica_provider_cache_misses` - Provider metadata cache instrumentation

### Orderbook
- `meltica_orderbook_buffer_size` - Buffer size per symbol
- `meltica_orderbook_gap_detected` - Sequence gaps detected
- `meltica_orderbook_snapshot_applied` - Snapshots applied
- `meltica_orderbook_coldstart_duration_bucket` - Coldstart latency

### Clients
- `meltica_wsclient_frames_processed` - WebSocket frames processed
- `meltica_wsclient_frame_processing_duration_bucket` - Frame processing latency
- `meltica_restclient_polls` - REST poll counter
- `meltica_restclient_poll_duration_bucket` - REST poll latency
- `meltica_restclient_snapshots_fetched` - Snapshots fetched via REST

### Control Bus
- `meltica_controlbus_consumers_active` - Active consumers
- `meltica_controlbus_queue_depth` - Command queue depth

### Extension Events
- `meltica_adapter_extension_events` - Adapter-level extension payload rate (labels: `environment`, `provider`, `symbol`)
- `meltica_adapter_extension_payload_bytes_sum` / `_count` - Helpers for computing average payload sizes (same labels as above)

## 🐛 Troubleshooting

### Dashboard shows "No data"
1. Verify Prometheus datasource is configured
2. Check time range (default: last 30 minutes)
3. Verify Meltica is running and exporting metrics to `http://capy.lan:8889/metrics`
4. Check Prometheus is scraping the OTLP collector

### Query performance issues
All queries are pre-optimized and tested. If you experience slow queries:
1. Reduce time range
2. Limit template variable selections (fewer providers/event types)
3. Check Prometheus resource usage

### Missing panels after import
Ensure you're using Grafana v10.0+ and have the Prometheus datasource configured correctly.
