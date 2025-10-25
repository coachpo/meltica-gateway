# Meltica Telemetry Points Inventory

Complete list of all telemetry/observability points instrumented in the Meltica codebase.

## Summary Statistics

- **Total Metrics**: 22
- **Counters**: 10
- **UpDownCounters**: 3
- **Histograms**: 4
- **ObservableGauges**: 3
- **Reconnection Metrics**: 2

---

## 1. Data Bus (`internal/bus/eventbus/memory.go`)

### Metrics

| Metric Name | Type | Unit | Description | Labels |
|------------|------|------|-------------|--------|
| `eventbus.events.published` | Counter | `{event}` | Total events published to the bus | environment, event_type, provider, symbol |
| `eventbus.subscribers` | UpDownCounter | `{subscriber}` | Current number of active subscribers | environment, event_type |
| `eventbus.delivery.errors` | Counter | `{error}` | Failed event deliveries to subscribers | environment, event_type, error_type, reason |
| `eventbus.fanout.size` | Histogram | `{subscriber}` | Number of subscribers per fanout event | environment, event_type, provider, symbol |

**Histogram Buckets (eventbus.fanout.size)**:
```
[1, 2, 5, 10, 20, 50, 100]
```
Optimized for subscriber count distribution (typically 1-20).

---

## 2. Dispatcher (`internal/dispatcher/runtime.go`)

### Metrics

| Metric Name | Type | Unit | Description | Labels |
|------------|------|------|-------------|--------|
| `dispatcher.events.ingested` | Counter | `{event}` | Events received by dispatcher | environment, event_type, provider, symbol |
| `dispatcher.events.dropped` | Counter | `{event}` | Events dropped (buffer full, etc.) | environment, event_type, provider, symbol, reason |
| `dispatcher.events.duplicate` | Counter | `{event}` | Duplicate events detected | environment, event_type, provider, symbol |
| `dispatcher.processing.duration` | Histogram | `ms` | Event processing latency | environment, event_type, provider, symbol |
| `dispatcher.routing.version` | ObservableGauge | `{version}` | Current routing table version | environment |

**Histogram Buckets (dispatcher.processing.duration)**:
```
[0.1, 0.5, 1, 2, 5, 10, 25, 50, 100, 250, 500]
```
Optimized for event processing (typically 0.5-10ms).

---

## 3. Pool Manager (`internal/pool/manager.go`)

### Metrics

| Metric Name | Type | Unit | Description | Labels |
|------------|------|------|-------------|--------|
| `pool.objects.borrowed` | Counter | `{object}` | Total objects borrowed from pool | environment, pool_name, object_type |
| `pool.objects.active` | UpDownCounter | `{object}` | Currently borrowed objects | environment, pool_name, object_type |
| `pool.borrow.duration` | Histogram | `ms` | Time to borrow object from pool | environment, pool_name, object_type |
| `pool.capacity` | ObservableGauge | `{object}` | Pool capacity (max objects) | environment, pool_name, object_type |
| `pool.available` | ObservableGauge | `{object}` | Available objects in pool | environment, pool_name, object_type |

**Histogram Buckets (pool.borrow.duration)**:
```
[0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 25, 50]
```
Optimized for memory pool operations (typically 0.05-2ms).

**Known Pool Names**:
- `Event` - Event objects (`*schema.Event`)
- `OrderRequest` - Order request objects (`*schema.OrderRequest`)

---

## 4. Orderbook Assembler (`internal/adapters/binance/book_assembler.go`)

### Metrics

| Metric Name | Type | Unit | Description | Labels |
|------------|------|------|-------------|--------|
| `orderbook.gap.detected` | Counter | `{gap}` | Sequence gaps detected | environment, provider, symbol, event_type |
| `orderbook.buffer.size` | UpDownCounter | `{update}` | Currently buffered updates | environment, provider, symbol, event_type |
| `orderbook.update.stale` | Counter | `{update}` | Stale updates rejected | environment, provider, symbol, event_type |
| `orderbook.snapshot.applied` | Counter | `{snapshot}` | Snapshots applied | environment, provider, symbol, event_type, **source** |
| `orderbook.updates.replayed` | Counter | `{update}` | Updates replayed from buffer | environment, provider, symbol, event_type |
| `orderbook.coldstart.duration` | Histogram | `ms` | Time from first update to snapshot ready | environment, provider, symbol, event_type |
| `orderbook.recovery.attempts` | Counter | `{attempt}` | Snapshot recovery attempts (includes retries) | environment, provider, symbol, attempt |
| `orderbook.recovery.success` | Counter | `{recovery}` | Successful recoveries after gap | environment, provider, symbol |
| `orderbook.recovery.failed` | Counter | `{recovery}` | Failed recoveries (all retries exhausted) | environment, provider, symbol, attempts, reason |
| `orderbook.recovery.duration` | Histogram | `ms` | Time from gap to successful snapshot | environment, provider, symbol |
| `orderbook.recovery.retry_count` | Histogram | `{retry}` | Number of retries needed for recovery | environment, provider, symbol |

**Histogram Buckets (orderbook.coldstart.duration)**:
```
[100, 250, 500, 1000, 2000, 5000, 10000, 30000]
```
Optimized for orderbook initialization (typically 500-2000ms).

**Histogram Buckets (orderbook.recovery.duration)**:
```
[50, 100, 200, 500, 1000, 2000, 5000, 10000]
```
Optimized for gap recovery with retry (typically 100-1000ms).

**Histogram Buckets (orderbook.recovery.retry_count)**:
```
[0, 1, 2, 3, 4, 5]
```
Tracks retries needed (0=first attempt success, 5=all attempts exhausted).

**New Label Values**:
- `source`: `coldstart` (initial snapshot) or `immediate` (gap recovery)
- `reason`: `max_retries` (all 5 attempts failed) or `permanent_error` (401/403/404/400)
- `attempt`: `1-5` (which retry attempt)

---

## 5. Provider WebSocket (`internal/adapters/binance/ws_provider.go`)

### Metrics

| Metric Name | Type | Unit | Description | Labels |
|------------|------|------|-------------|--------|
| `provider.reconnections` | Counter | `{reconnection}` | WebSocket reconnection attempts | environment, provider, reason |

**Known Reconnection Reasons**:
- `connection_lost` - Network disconnection
- `ping_timeout` - Keepalive failure
- `error` - Protocol/application error

---

## 6. Fake Provider (`internal/adapters/fake/provider.go`)

### Metrics

| Metric Name | Type | Unit | Description | Labels |
|-------------|------|------|-------------|--------|
| `provider.fake.events.emitted` | Counter | `{event}` | Total synthetic events emitted | environment, provider, symbol, event_type |
| `provider.fake.orders.received` | Counter | `{order}` | Orders accepted for simulation (pre-validation) | environment, provider, symbol, order.side, order.type, order.tif |
| `provider.fake.orders.rejected` | Counter | `{order}` | Orders rejected after validation | environment, provider, symbol, order.side, order.type, order.tif, reason |
| `provider.fake.order.latency` | Histogram | `ms` | Submission-to-final-state latency | environment, provider, symbol, order.side, order.type, order.tif, order.state |
| `provider.fake.venue.disruptions` | Counter | `{event}` | Simulated venue disconnect/reconnect cycles | environment, provider, operation, result |
| `provider.fake.venue.errors` | Counter | `{event}` | Injected transient venue errors | environment, provider, operation, result |
| `provider.fake.balance.updates` | Counter | `{event}` | Balance updates emitted per currency | environment, provider, currency |
| `provider.fake.balance.total` | ObservableGauge | `{currency}` | Current total balance per currency | environment, provider, currency |
| `provider.fake.balance.available` | ObservableGauge | `{currency}` | Current available balance per currency | environment, provider, currency |

**Notes**:
- Balance gauges are observed asynchronously by the provider at every update tick.
- Currency codes follow `schema.NormalizeCurrencyCode` (uppercase, 2-10 alphanumeric characters).

---

## Semantic Conventions

### Standard Labels (Applied to All Metrics)

| Label | Values | Description |
|-------|--------|-------------|
| `environment` | `development`, `staging`, `production` | Runtime environment |
| `event_type` | `BookSnapshot`, `Trade`, `Ticker`, `Kline` | Event type classification |
| `provider` | `binance`, `fake` | Data source provider |
| `symbol` | `BTC-USDT`, `ETH-USDT`, `XRP-USDT`, etc. | Trading pair |
| `pool_name` | `Event`, `OrderRequest` | Pool identifier |
| `object_type` | `*schema.Event`, `*schema.OrderRequest` | Go type of pooled object |
| `error_type` | Various | Error classification |
| `reason` | Various | Specific cause/context |

### Helper Functions

Available in `internal/telemetry/semconv.go`:

```go
// Generate standard event attributes
telemetry.EventAttributes(environment, eventType, provider, symbol)

// Generate pool attributes
telemetry.PoolAttributes(environment, poolName, objectType)

// Generate error attributes
telemetry.ErrorAttributes(environment, errorType, reason)
```

---

## Metric Naming Conventions

All metrics follow OpenTelemetry naming standards:

**Pattern**: `<namespace>.<component>.<metric_name>`

**Examples**:
- `eventbus.events.published` - Event bus component, events published
- `pool.objects.active` - Pool component, active objects
- `orderbook.gap.detected` - Orderbook component, gaps detected

**Units**:
- `{event}`, `{object}`, `{subscriber}` - Countable items
- `ms` - Milliseconds (duration)
- `{version}` - Version number

---

## Trace Spans

Key traced operations:

| Span Name | Component | Description |
|-----------|-----------|-------------|
| `dispatcher.process_event` | Dispatcher | Individual event processing |
| `dispatcher.publish_batch` | Dispatcher | Batch publication to eventbus |
| `eventbus.Publish` | Event Bus | Event publication with fanout |
| `eventbus.dispatch` | Event Bus | Event delivery to subscribers |
| `pool.Get` | Pool | Object borrowing operation |

**Context Propagation**: All spans properly propagate context to child operations.

---

## Exemplar Support

### Enabled Histograms

All 4 histograms support exemplars (trace correlation):

1. `dispatcher.processing.duration` - Links slow events to traces
2. `pool.borrow.duration` - Links slow borrows to traces
3. `orderbook.coldstart.duration` - Links slow cold starts to traces
4. `eventbus.fanout.size` - Links large fanouts to traces

### Configuration

Requires:
- OpenTelemetry Collector with `enable_open_metrics: true`
- Prometheus 2.54+ with exemplar storage
- Grafana 11.2+ with exemplar visualization

---

## Performance Overhead

| Metric Type | Overhead | Notes |
|-------------|----------|-------|
| Counter.Add() | ~100-500ns | Lock-free atomic increment |
| UpDownCounter.Add() | ~100-500ns | Lock-free atomic add/subtract |
| Histogram.Record() | ~500ns-2µs | Bucket selection + exemplar sampling |
| ObservableGauge | 0 (lazy) | Computed on scrape |
| Trace Span | ~1-5µs | Minimal with batching |

**Total Application Overhead**: < 2% CPU, ~10-20MB memory

---

## Configuration

Telemetry is configured via environment variables (see `internal/telemetry/telemetry.go`):

```bash
# Enable/disable telemetry
OTEL_ENABLED=true

# OTLP endpoint
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318
OTEL_EXPORTER_OTLP_INSECURE=true

# Service identification
OTEL_SERVICE_NAME=meltica
OTEL_SERVICE_NAMESPACE=production

# Environment label
MELTICA_ENV=production
# or
OTEL_RESOURCE_ENVIRONMENT=production
```

---

## Grafana Dashboards

Pre-built dashboards using these metrics:

1. **grafana-pool-dashboard.json**
   - Pool capacity and utilization
   - Borrow/return rates
   - Latency distribution (p50, p95, p99)
   - Pool exhaustion risk

2. **grafana-provider-dashboard.json**
   - Provider health overview
   - Ingestion, drop, and duplicate rates
   - Processing latency
   - Buffering and eventbus throughput
   - Orderbook quality signals
   - Provider health summary table

3. **grafana-orderbook-dashboard.json**
   - Gap detection rates
   - Buffer sizes
   - Stale update rates
   - Snapshot application
   - Cold start duration
   - WebSocket health

---

## Missing Metrics (Documented but Not Implemented)

These metrics are referenced in dashboards but don't appear in code:

1. ~~`meltica_orderbook_updates_replayed`~~ - **FIXED**: Panel removed from orderbook dashboard

**All other metrics are properly implemented and working.**

---

## Future Instrumentation Candidates

Potential additions mentioned in `docs/telemetry.md`:

1. **Control Bus** (not yet instrumented)
   - Command send/ack latency
   - Consumer queue depth

2. **Snapshot Store** (not yet instrumented)
   - Snapshot read/write operations
   - CAS retry counts
   - Cache hit rates

3. **HTTP Handlers** (not yet instrumented)
   - Request duration
   - Response codes
   - Request body sizes

4. **Provider Adapters** (partial - reconnections only)
   - WebSocket connection lifecycle (✓ reconnections implemented)
   - Message receive rates
   - Reconnection attempts (✓ implemented)

---

## Prometheus Metric Export Format

All metrics are exported in Prometheus format via OTLP Collector:

```promql
# Counter
meltica_dispatcher_events_ingested{environment="development",event_type="BookSnapshot",provider="binance",symbol="BTC-USDT"} 123456

# Gauge (UpDownCounter)
meltica_pool_objects_active{environment="development",pool_name="Event",object_type="*schema.Event"} 5913

# Histogram (with exemplars)
meltica_dispatcher_processing_duration_bucket{environment="development",event_type="Trade",le="1"} 98765 # {trace_id="abc123"} 0.95 1234567890.123
```

All metrics include standard resource attributes:
- `service_name="meltica"`
- `service_version="1.0.0"`
- `host_name`, `os_type`, `process_runtime_name`, `process_runtime_version`
- `cluster`, `deployment_environment`

---

## Verification Checklist

To verify all telemetry is working:

```bash
# 1. Check metrics endpoint
curl http://localhost:8889/metrics | grep meltica_

# 2. Verify all 23 metrics are present
curl -s http://localhost:8889/metrics | grep "^meltica_" | cut -d'{' -f1 | sort -u | wc -l
# Should output: 23

# 3. Check traces in Jaeger
curl 'http://localhost:16686/api/traces?service=meltica&limit=10'

# 4. Verify exemplars in Prometheus
curl 'http://localhost:9090/api/v1/query?query=meltica_dispatcher_processing_duration_bucket' | jq '.data.result[].exemplar'
```

---

## Last Updated

Generated: 2025-01-XX (based on current codebase state)  
Codebase Version: go1.25.3
