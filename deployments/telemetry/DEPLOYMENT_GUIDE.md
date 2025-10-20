# Meltica Telemetry Deployment Guide

## Your Setup

```
┌─────────────────────┐                    ┌─────────────────────────────┐
│   Your Laptop       │                    │   Server (capy.lan)         │
│                     │                    │                             │
│  ┌───────────────┐  │  OTLP HTTP/4318   │  ┌───────────────────────┐  │
│  │   Meltica     │──┼───────────────────>│  │  OTLP Collector       │  │
│  │   Gateway     │  │                    │  │  Port: 4318           │  │
│  └───────────────┘  │                    │  └──────────┬────────────┘  │
│                     │                    │             │               │
└─────────────────────┘                    │             │ remote_write  │
                                           │             ↓               │
                                           │  ┌───────────────────────┐  │
                                           │  │  Prometheus           │  │
                                           │  │  Port: 9090           │  │
                                           │  └───────────────────────┘  │
                                           └─────────────────────────────┘
```

## Step 1: Configure Meltica on Your Laptop

**File:** `config/app.yaml` (already correct ✅)

```yaml
telemetry:
  otlpEndpoint: http://capy.lan:4318   # Points to OTLP Collector on server
  serviceName: meltica-gateway
  otlpInsecure: true
  enableMetrics: true  # Metrics only - no traces
```

This tells Meltica to send metrics to the OTLP Collector running on your server.

## Step 2: Deploy OTLP Collector on Server (capy.lan)

### Option A: Using Docker (Recommended)

**1. Copy the config to your server:**
```bash
# From your laptop
scp /home/qing/work/meltica/deployments/telemetry/otel-collector-config.yaml \
    <user>@capy.lan:~/otel-collector-config.yaml
```

**2. Run OTLP Collector on capy.lan:**
```bash
# SSH to your server
ssh <user>@capy.lan

# Run OTLP Collector
docker run -d \
  --name otel-collector \
  --network host \
  -v ~/otel-collector-config.yaml:/etc/otel-collector-config.yaml:ro \
  otel/opentelemetry-collector-contrib:0.105.0 \
  --config=/etc/otel-collector-config.yaml
```

### Option B: Binary Installation

**1. Download on server:**
```bash
ssh <user>@capy.lan

# Download collector
wget https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.105.0/otelcol-contrib_0.105.0_linux_amd64.tar.gz
tar xzf otelcol-contrib_0.105.0_linux_amd64.tar.gz
```

**2. Copy config and run:**
```bash
# Copy config from laptop
scp /home/qing/work/meltica/deployments/telemetry/otel-collector-config.yaml \
    <user>@capy.lan:~/otel-collector-config.yaml

# Run collector
ssh <user>@capy.lan
./otelcol-contrib --config=otel-collector-config.yaml
```

### Option C: Systemd Service

**1. Create service file on server:**
```bash
ssh <user>@capy.lan
sudo nano /etc/systemd/system/otel-collector.service
```

**Content:**
```ini
[Unit]
Description=OpenTelemetry Collector
After=network.target

[Service]
Type=simple
User=otel
ExecStart=/usr/local/bin/otelcol-contrib --config=/etc/otel-collector-config.yaml
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

**2. Enable and start:**
```bash
sudo systemctl daemon-reload
sudo systemctl enable otel-collector
sudo systemctl start otel-collector
```

## Step 3: Verify Connection from Laptop to Server

**Test 1: Check OTLP Collector is reachable**
```bash
# From your laptop
curl -v http://capy.lan:4318/v1/metrics

# Should return 404 or 405 (expected - endpoint exists but expects POST)
```

**Test 2: Check OTLP Collector is running**
```bash
# From your laptop
curl http://capy.lan:13133/

# Should return collector health check
```

**Test 3: Start Meltica and check logs**
```bash
# From your laptop
./bin/gateway -config config/app.yaml

# Look for:
# "telemetry initialized: endpoint=http://capy.lan:4318, service=meltica-gateway"
```

## Step 4: Verify Metrics Flow

**On Server (capy.lan):**

```bash
ssh <user>@capy.lan

# Check OTLP Collector logs
docker logs otel-collector 2>&1 | grep -i "remote\|error"

# Or if running as binary:
journalctl -u otel-collector -f

# Check Prometheus has data
curl 'http://localhost:9090/api/v1/query?query=up{service="meltica-gateway"}'
```

**From Your Laptop:**

```bash
# Query Prometheus on server
curl 'http://capy.lan:9090/api/v1/query?query=meltica_eventbus_events_published_total'

# Or visit in browser:
# http://capy.lan:9090/graph
```

## Firewall Configuration

Make sure these ports are open on capy.lan:

```bash
# On capy.lan server
sudo firewall-cmd --permanent --add-port=4318/tcp  # OTLP Collector
sudo firewall-cmd --permanent --add-port=9090/tcp  # Prometheus (for queries)
sudo firewall-cmd --reload

# Or for iptables:
sudo iptables -A INPUT -p tcp --dport 4318 -j ACCEPT
sudo iptables -A INPUT -p tcp --dport 9090 -j ACCEPT
sudo iptables-save
```

## Configuration Files Reference

### Laptop: `config/app.yaml`
```yaml
telemetry:
  otlpEndpoint: http://capy.lan:4318  # Server address
  serviceName: meltica-gateway
  otlpInsecure: true
  enableMetrics: true
```

### Server: `otel-collector-config.yaml`
```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318  # Listen on all interfaces

exporters:
  prometheusremotewrite:
    endpoint: http://localhost:9090/api/v1/write  # Local Prometheus
    tls:
      insecure: true

service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, resourcedetection, batch, transform, resource]
      exporters: [prometheusremotewrite, prometheus, logging]
```

## Troubleshooting

### Meltica can't reach OTLP Collector

**Symptom:** Errors in Meltica logs about connection refused

**Check:**
```bash
# From laptop, test connectivity
telnet capy.lan 4318
# or
nc -zv capy.lan 4318

# Check firewall on server
ssh <user>@capy.lan
sudo netstat -tlnp | grep 4318
```

**Fix:**
- Ensure OTLP Collector is running on server
- Check firewall allows port 4318
- Verify `capy.lan` resolves correctly from laptop

### OTLP Collector receives but Prometheus has no data

**Check:**
```bash
# On server
curl http://localhost:9090/api/v1/query?query=up

# Check collector logs
docker logs otel-collector | grep -i error
```

**Common issues:**
- Prometheus not running: `sudo systemctl status prometheus`
- Wrong endpoint in config: Should be `localhost:9090` not `capy.lan:9090`
- Prometheus remote_write not enabled

### No metrics showing in queries

**Wait 30 seconds** - There's batching delay

**Then check:**
```bash
# List all metric names
curl http://capy.lan:9090/api/v1/label/__name__/values | grep meltica

# Check specific metric
curl 'http://capy.lan:9090/api/v1/query?query=meltica_eventbus_events_published_total'
```

## Performance Tuning

For high-throughput scenarios, adjust on server:

**`otel-collector-config.yaml`:**
```yaml
processors:
  batch:
    timeout: 5s          # Faster batching
    send_batch_size: 2048
    
  memory_limiter:
    limit_mib: 1024      # More memory
    spike_limit_mib: 256
```

## Security Considerations

**For production:**

1. **Enable TLS:**
   - Get certificates for capy.lan
   - Update `config/app.yaml`: `otlpInsecure: false`
   - Configure OTLP Collector with TLS certs

2. **Add authentication:**
   - Use API keys or mTLS
   - Restrict Prometheus access

3. **Network security:**
   - Use VPN or SSH tunnel for laptop → server
   - Firewall rules to limit access
