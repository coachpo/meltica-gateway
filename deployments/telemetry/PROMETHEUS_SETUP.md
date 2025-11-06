# Adding Meltica to Your Prometheus

Your Prometheus instance at `http://capy.lan:9090` needs to scrape metrics from the OTLP Collector.

## Quick Setup

### 1. Add Scrape Configuration

Edit your Prometheus configuration file (usually `/etc/prometheus/prometheus.yml`):

```yaml
scrape_configs:
  # ... your existing scrape configs ...

  # Add Meltica metrics
  - job_name: 'meltica'
    scrape_interval: 15s
    static_configs:
      - targets: ['capy.lan:8889']
        labels:
          service: 'meltica-gateway'
          environment: 'development'
```

### 2. Reload Prometheus

```bash
# If using systemd
sudo systemctl reload prometheus

# Or send SIGHUP
kill -HUP $(pgrep prometheus)

# Or restart
sudo systemctl restart prometheus
```

### 3. Verify Scraping

1. Go to http://capy.lan:9090/targets
2. Look for the `meltica` job
3. Status should show "UP" with green indicator
4. Click "show more" to see scraped metrics

### 4. Test Queries

In Prometheus UI (http://capy.lan:9090/graph), try these queries:

```promql
# Check if metrics are arriving
up{job="meltica"}

# Event throughput
rate(meltica_eventbus_events_published_total[1m])

# Active pool objects
meltica_pool_objects_active

# Processing latency (95th percentile)
histogram_quantile(0.95, rate(meltica_dispatcher_processing_duration_bucket[5m]))

# Database pool usage
meltica_db_pool_connections_acquired

# Migration executions in the last hour
increase(meltica_db_migrations_total[1h])
```

## Alternative: Prometheus Remote Write

If you prefer push-based metrics instead of scraping, configure the OTLP Collector to use remote_write:

### 1. Edit `otel-collector-config.yaml`

Replace the `prometheus` exporter with `prometheusremotewrite`:

```yaml
exporters:
  prometheusremotewrite:
    endpoint: http://capy.lan:9090/api/v1/write
    tls:
      insecure: true
    # Optional: Add basic auth if needed
    # auth:
    #   authenticator: basicauth/prometheus

service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, resourcedetection, batch, transform, resource]
      exporters: [prometheusremotewrite, logging]  # Changed from 'prometheus'
```

### 2. Restart OTLP Collector

```bash
# If using Docker
docker restart meltica-otel-collector

# Or if running as a service
sudo systemctl restart otel-collector
```

## Troubleshooting

### Scrape target shows "DOWN"

**Check network connectivity:**
```bash
curl http://capy.lan:8889/metrics
```

Should return Prometheus metrics format.

**Common issues:**
- Firewall blocking port 8889
- OTLP Collector not running
- Wrong hostname/IP in Prometheus config

### No Meltica metrics showing

**Check OTLP Collector is receiving data:**
```bash
curl http://capy.lan:8889/metrics | grep meltica
```

If empty, Meltica may not be sending to OTLP Collector.

**Verify Meltica configuration:**
```yaml
# In config/app.yaml
telemetry:
  otlpEndpoint: http://capy.lan:4318
  serviceName: meltica-gateway
  otlpInsecure: true
  enableMetrics: true
```

**Check Meltica logs:**
```
gateway ... telemetry initialized: endpoint=http://capy.lan:4318, service=meltica-gateway
```

### Metrics showing but with wrong labels

The OTLP Collector's `transform` processor normalizes labels. Check `otel-collector-config.yaml`:

```yaml
processors:
  transform:
    error_mode: ignore
    metric_statements:
      - context: resource
        statements:
          # This normalizes the environment attribute
          - set(attributes["deployment.environment"], attributes["environment"]) where attributes["environment"] != nil
```

## Available Metrics

Once configured, you'll see these metric families:

### Event Bus Metrics
- `meltica_eventbus_events_published_total` - Total events published
- `meltica_eventbus_subscribers` - Active subscribers per event type
- `meltica_eventbus_delivery_errors_total` - Delivery failures
- `meltica_eventbus_fanout_size` - Subscribers per fanout

### Dispatcher Metrics
- `meltica_dispatcher_events_ingested_total` - Events received
- `meltica_dispatcher_events_dropped_total` - Dropped events
- `meltica_dispatcher_events_duplicate_total` - Duplicate events
- `meltica_dispatcher_processing_duration` - Processing latency histogram

### Pool Metrics
- `meltica_pool_objects_borrowed_total` - Objects borrowed from pools
- `meltica_pool_objects_active` - Currently active pooled objects
- `meltica_pool_borrow_duration` - Time to acquire objects
- `meltica_pool_capacity` - Total pool capacity
- `meltica_pool_available` - Available objects in pool

### Database & Persistence Metrics
- `meltica_db_pool_connections_total` - Total pgx connections (idle + acquired + constructing)
- `meltica_db_pool_connections_idle` - Idle connections
- `meltica_db_pool_connections_acquired` - In-use connections
- `meltica_db_pool_connections_constructing` - Connections being created
- `meltica_db_migrations_total` - Migration executions (attr `result`)
- `meltica_provider_cache_hits` / `meltica_provider_cache_misses` - Provider metadata cache instrumentation

### Control Bus Metrics
- `meltica_controlbus_send_duration` - Control command latency
- `meltica_controlbus_queue_depth` - Control queue depth
- `meltica_controlbus_send_errors_total` - Control send failures

### WebSocket Client Metrics
- `meltica_wsclient_frames_processed_total` - WebSocket frames processed
- `meltica_wsclient_frame_errors_total` - Frame processing errors
- `meltica_wsclient_frame_latency` - Frame processing latency

All metrics include labels like:
- `environment` - Runtime environment (development, production)
- `provider` - Data provider name (binance, etc)
- `event_type` - Type of event (TICKER, TRADE, ORDERBOOK_DELTA)
- `symbol` - Trading pair symbol (BTC-USDT, etc)
