# Meltica Metrics Architecture

## Current Setup

**Deployment:**
- **Meltica Gateway**: Runs on your laptop
- **OTLP Collector**: Runs on server at capy.lan:4318
- **Prometheus**: Runs on server at capy.lan:9090 (same machine as OTLP Collector)

```
┌─────────────────────┐                    ┌─────────────────────────────┐
│   Your Laptop       │                    │   Server (capy.lan)         │
│                     │                    │                             │
│  ┌──────────────┐   │  OTLP HTTP/4318   │  ┌───────────────────────┐  │
│  │   Meltica    │───┼───────────────────>│  │  OTLP Collector       │  │
│  │   Gateway    │   │                    │  │  Listens: 0.0.0.0:4318│  │
│  └──────────────┘   │                    │  └──────────┬────────────┘  │
│                     │                    │             │               │
└─────────────────────┘                    │             │ remote_write  │
                                           │             │ localhost:9090│
                                           │             ↓               │
                                           │  ┌───────────────────────┐  │
                                           │  │  Prometheus           │  │
                                           │  │  Port: 9090           │  │
                                           │  └───────────────────────┘  │
                                           └─────────────────────────────┘
```

## Two Ways Metrics Reach Prometheus

### Method 1: Remote Write (Push) ✅ **Recommended**

**How it works:**
1. Meltica sends OTLP metrics → OTLP Collector (port 4318)
2. OTLP Collector pushes → Prometheus (port 9090/api/v1/write)

**Advantages:**
- No configuration needed on Prometheus side
- Metrics arrive immediately
- Works through firewalls/NAT
- Simpler for distributed setups

**Current status:** ✅ Configured in `otel-collector-config.yaml`

**To verify it's working:**
```bash
# Check OTLP Collector logs
docker logs meltica-otel-collector 2>&1 | grep remote

# Query Prometheus
curl 'http://capy.lan:9090/api/v1/query?query=up{job="meltica"}'
```

### Method 2: Scraping (Pull)

**How it works:**
1. Meltica sends OTLP metrics → OTLP Collector (port 4318)
2. OTLP Collector exposes metrics → Port 8889 (Prometheus format)
3. Prometheus scrapes ← Port 8889

**Advantages:**
- Standard Prometheus pattern
- Prometheus controls scrape rate
- Can scrape multiple OTLP Collectors

**Setup required:** Add to `prometheus.yml`:
```yaml
scrape_configs:
  - job_name: 'meltica'
    static_configs:
      - targets: ['capy.lan:8889']
```

**To verify it's working:**
```bash
# Check endpoint is exposing metrics
curl http://capy.lan:8889/metrics | grep meltica

# Check Prometheus targets
# Visit: http://capy.lan:9090/targets
```

## Why OTLP Collector in the Middle?

The OTLP Collector provides:

1. **Protocol Translation**
   - Receives: OTLP format (binary)
   - Outputs: Prometheus format (text)

2. **Metric Processing**
   - Resource detection (adds host, container labels)
   - Batching for efficiency
   - Memory limiting
   - Label transformation

3. **Flexibility**
   - Can export to multiple backends
   - Can filter/sample metrics
   - Decouples Meltica from Prometheus

## Configuration Files

### Meltica Side: `config/app.yaml` (on your laptop)
```yaml
telemetry:
  otlpEndpoint: http://capy.lan:4318  # Send to OTLP Collector on server
  serviceName: meltica-gateway
  otlpInsecure: true
  enableMetrics: true
```

### OTLP Collector: `otel-collector-config.yaml` (on server capy.lan)
```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318  # Listen on all interfaces for laptop

exporters:
  prometheusremotewrite:
    endpoint: http://localhost:9090/api/v1/write  # Push to local Prometheus
  prometheus:
    endpoint: 0.0.0.0:8889  # Expose for scraping (optional)

service:
  pipelines:
    metrics:
      receivers: [otlp]
      exporters: [prometheusremotewrite, prometheus]
```

## Direct Meltica → Prometheus?

**Q: Why not skip the OTLP Collector and have Meltica expose :8889 directly?**

**A:** Possible, but requires code changes:
- Meltica currently only speaks OTLP protocol
- Would need to add Prometheus exporter library
- Loses benefits of OTLP Collector (batching, transformation, etc.)

The current architecture is **industry standard** for cloud-native monitoring.

## Troubleshooting

### No metrics in Prometheus

**Check 1: Is Meltica sending to OTLP Collector?**
```bash
# Look for telemetry initialization
./bin/gateway -config config/app.yaml 2>&1 | grep telemetry
# Should see: "telemetry initialized: endpoint=http://capy.lan:4318"
```

**Check 2: Is OTLP Collector receiving?**
```bash
# Check collector metrics
curl http://capy.lan:8889/metrics | grep otelcol_receiver
# Should see received spans/metrics counts
```

**Check 3: Is OTLP Collector pushing to Prometheus?**
```bash
# Check collector logs
docker logs meltica-otel-collector 2>&1 | grep -i "remote\|error"

# Check Prometheus received it
curl 'http://capy.lan:9090/api/v1/query?query=meltica_eventbus_events_published_total'
```

### High latency/resource usage

**OTLP Collector batching:** Edit `otel-collector-config.yaml`
```yaml
processors:
  batch:
    timeout: 30s        # Increase from 10s
    send_batch_size: 512  # Decrease from 1024
```

**Memory limits:**
```yaml
processors:
  memory_limiter:
    limit_mib: 256      # Decrease from 512
    spike_limit_mib: 64 # Decrease from 128
```
