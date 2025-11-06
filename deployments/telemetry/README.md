# Meltica Telemetry Stack

This directory contains observability configuration for Meltica using OpenTelemetry and Prometheus for metrics collection.

## Quick Start

### Option 1: Direct Prometheus Scraping (Recommended)

Configure your Prometheus at `http://capy.lan:9090` to scrape metrics from the OTLP Collector:

Add this to your Prometheus `prometheus.yml`:
```yaml
scrape_configs:
  - job_name: 'meltica'
    static_configs:
      - targets: ['capy.lan:8889']  # OTLP Collector metrics endpoint
```

Then configure and start Meltica:
```bash
# Using config/app.yaml (already configured)
./bin/gateway --config=config/app.yaml
```

The `config/app.yaml` file is already configured with:
```yaml
telemetry:
  otlpEndpoint: http://capy.lan:4318
  serviceName: meltica-gateway
  otlpInsecure: true
  enableMetrics: true  # Metrics only - no traces
```

### Option 2: Prometheus Remote Write

To push metrics directly to your Prometheus, update `otel-collector-config.yaml` (see Configuration section below).

**Access:**
- **Your Prometheus**: http://capy.lan:9090
- **OTLP Collector Health**: http://capy.lan:13133
- **OTLP Collector Metrics**: http://capy.lan:8889/metrics

## Architecture

```
Meltica Gateway
    ↓ (OTLP HTTP)
OTLP Collector (capy.lan:4318)
    ↓ (Prometheus exposition format)
    Port 8889 ← Prometheus scrapes here
    ↓
Your Prometheus (capy.lan:9090)
```

## Configuration

### OTLP Collector Ports
- **4317**: OTLP gRPC receiver
- **4318**: OTLP HTTP receiver (Meltica sends here)
- **8889**: Prometheus metrics endpoint (Prometheus scrapes here)
- **13133**: Health check endpoint

### Using Prometheus Remote Write (Alternative)

If you prefer push-based metrics, edit `otel-collector-config.yaml`:

```yaml
exporters:
  prometheusremotewrite:
    endpoint: http://capy.lan:9090/api/v1/write
    tls:
      insecure: true

service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, resourcedetection, batch, transform, resource]
      exporters: [prometheusremotewrite, logging]
```

Then restart the OTLP collector.

## Useful Prometheus Queries

### Event Throughput
```promql
# Events published per second by type
rate(meltica_eventbus_events_published_total[1m])

# Events ingested by dispatcher
rate(meltica_dispatcher_events_ingested_total[1m])
```

### Error Rates
```promql
# Delivery errors per second
rate(meltica_eventbus_delivery_errors_total[1m])

# Dropped events per second
rate(meltica_dispatcher_events_dropped_total[1m])
```

### Latency Metrics
```promql
# 95th percentile event processing duration
histogram_quantile(0.95, rate(meltica_dispatcher_processing_duration_bucket[5m]))

# 99th percentile pool borrow duration
histogram_quantile(0.99, rate(meltica_pool_borrow_duration_bucket[5m]))
```

### Resource Utilization
```promql
# Active subscribers by event type
meltica_eventbus_subscribers

# Active pooled objects by pool name
meltica_pool_objects_active
```

### Operational Metrics
```promql
# Duplicate event rate
rate(meltica_dispatcher_events_duplicate_total[1m])

# Fanout distribution (average subscribers per event)
avg(meltica_eventbus_fanout_size)

# Database pool usage
meltica_db_pool_connections_acquired

# Migration executions in the last 6h grouped by result
sum(increase(meltica_db_migrations_total[6h])) by (result)
```

## Creating Dashboards

Using Prometheus at http://capy.lan:9090 or Grafana, create dashboards with these queries:
- **Event Throughput Panel:**
  - Query: `sum(rate(meltica_eventbus_events_published_total[1m])) by (event_type)`
  
- **Pool Utilization Panel:**
  - Query: `meltica_pool_objects_active`
  
- **Processing Latency Panel:**
  - Query: `histogram_quantile(0.95, rate(meltica_dispatcher_processing_duration_bucket[5m]))`

## Troubleshooting

### No metrics in Prometheus
1. **Check OTLP Collector is running:**
   ```bash
   curl http://capy.lan:13133
   ```

2. **Check OTLP Collector is exposing metrics:**
   ```bash
   curl http://capy.lan:8889/metrics | grep meltica
   ```

3. **Verify Prometheus is scraping:**
   - Go to http://capy.lan:9090/targets
   - Look for the `meltica` job
   - Status should be "UP"

4. **Check Meltica telemetry logs:**
   ```
   gateway ... telemetry initialized: endpoint=http://capy.lan:4318, service=meltica-gateway
   ```

5. **Test OTLP endpoint connectivity:**
   ```bash
   curl -v http://capy.lan:4318/v1/metrics
   ```

### High resource usage
1. Reduce batch size in `otel-collector-config.yaml`
2. Increase batch timeout to reduce frequency
3. Adjust memory limiter settings

## Production Considerations

For production deployments:

1. **Security:**
   - Enable TLS for OTLP endpoints
   - Use authentication for Prometheus
   - Restrict network access with firewall rules
   - Use mTLS for collector-to-Prometheus communication

2. **Scaling:**
   - Deploy OTEL Collector as a daemonset/sidecar
   - Use Prometheus remote_write for horizontal scaling
   - Consider managed services (e.g., Grafana Cloud)

3. **Retention:**
   - Set appropriate Prometheus retention period
   - Use long-term storage (Thanos, Cortex, Victoria Metrics)
   - Configure data compaction settings

4. **High Availability:**
   - Deploy multiple OTEL Collectors with load balancing
   - Use Prometheus federation or remote_write replication
   - Implement collector failover strategies

5. **Performance:**
   - Tune batch processor settings for your workload
   - Use memory limiter to prevent OOM
   - Monitor collector performance metrics
