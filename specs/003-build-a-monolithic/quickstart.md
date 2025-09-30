# Quickstart Guide: Monolithic Auto-Trading Application

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)  
**Date**: October 12, 2025  
**Status**: Developer Onboarding Guide

## Overview

This guide walks you through setting up, configuring, and running the monolithic auto-trading application locally. You'll learn how to:

1. Set up the development environment
2. Configure providers and consumers
3. Run the monolith
4. Subscribe to market data
5. Submit orders
6. Monitor telemetry

**Target Audience**: Developers implementing or extending the trading monolith.

---

## Prerequisites

- **Go 1.25+** installed ([download](https://go.dev/dl/))
- **Git** for cloning the repository
- **Exchange API credentials** (Binance, Coinbase, Kraken) for provider connections
- **Make** (optional, for convenience commands)

---

## 1. Clone and Build

```bash
# Clone the repository
git clone https://github.com/your-org/meltica.git
cd meltica

# Checkout the feature branch
git checkout 003-build-a-monolithic

# Install dependencies
go mod download

# Build the monolith
make build
# or
go build -o bin/gateway ./cmd/gateway

# Verify build
./bin/gateway --version
```

---

## 2. Configuration

The monolith uses YAML configuration (`config/streaming.yaml`). Create your config file from the example:

```bash
cp config/streaming.example.yaml config/streaming.yaml
```

### Example Configuration

```yaml
# config/streaming.yaml

# Provider Configuration
providers:
  - name: binance
    ws_endpoint: wss://stream.binance.com:9443/ws
    rest_endpoint: https://api.binance.com
    api_key: YOUR_BINANCE_API_KEY        # Set via environment variable
    api_secret: YOUR_BINANCE_API_SECRET  # Set via environment variable
    symbols: ["BTC-USDT", "ETH-USDT", "SOL-USDT"]
    book_refresh_interval: 3m            # Periodic REST snapshot refresh
    
  - name: coinbase
    ws_endpoint: wss://ws-feed.exchange.coinbase.com
    rest_endpoint: https://api.exchange.coinbase.com
    api_key: YOUR_COINBASE_API_KEY
    api_secret: YOUR_COINBASE_API_SECRET
    symbols: ["BTC-USD", "ETH-USD"]
    book_refresh_interval: 2m

  - name: kraken
    ws_endpoint: wss://ws.kraken.com
    rest_endpoint: https://api.kraken.com
    api_key: YOUR_KRAKEN_API_KEY
    api_secret: YOUR_KRAKEN_API_SECRET
    symbols: ["XBT-USD", "ETH-USD"]
    book_refresh_interval: 3m

# Orchestrator Configuration
orchestrator:
  merge_rules:
    # Merged subscription: BTC order books from multiple providers
    - merge_key: "BTC-USDT:BookSnapshot"
      providers: ["binance", "coinbase"]
      window_duration: 10s
      window_max_events: 1000
      partial_policy: suppress    # Drop partial windows (missing providers)
      
    - merge_key: "ETH-USDT:BookSnapshot"
      providers: ["binance", "coinbase"]
      window_duration: 10s
      window_max_events: 1000
      partial_policy: suppress

# Dispatcher Configuration
dispatcher:
  stream_ordering:
    lateness_tolerance: 150ms    # Buffer events up to 150ms out-of-order
    flush_interval: 50ms         # Flush ordered events every 50ms
    max_buffer_size: 200         # Max 200 events per stream buffer
  
  backpressure:
    token_rate_per_stream: 1000  # 1000 events/sec per stream
    token_burst: 100             # Allow bursts up to 100 events
  
  coalescable_types: ["Ticker", "BookUpdate", "KlineSummary"]

# Consumer Configuration
consumers:
  - name: strategy_alpha
    consumer_id: strategy-alpha
    trading_switch: enabled      # Enable trading for this consumer
    subscriptions:
      # Direct subscription to single provider
      - symbol: "BTC-USDT"
        providers: ["binance"]
        event_types: ["BookUpdate", "Trade"]
      
      # Merged subscription to multiple providers
      - symbol: "ETH-USDT"
        providers: ["binance", "coinbase"]
        merged: true
        event_types: ["BookSnapshot"]
  
  - name: strategy_beta
    consumer_id: strategy-beta
    trading_switch: disabled     # Disable trading for this consumer
    subscriptions:
      - symbol: "SOL-USDT"
        providers: ["binance"]
        event_types: ["Ticker", "Trade"]

# Telemetry Configuration (ops-only)
telemetry:
  enabled: true
  log_level: info              # info, warn, error
  metrics_port: 9090           # Prometheus metrics endpoint
  trace_sampling: 1.0          # 100% trace sampling (lower in production)
```

### Environment Variables

Set API credentials via environment variables:

```bash
export BINANCE_API_KEY="your-binance-api-key"
export BINANCE_API_SECRET="your-binance-api-secret"
export COINBASE_API_KEY="your-coinbase-api-key"
export COINBASE_API_SECRET="your-coinbase-api-secret"
export KRAKEN_API_KEY="your-kraken-api-key"
export KRAKEN_API_SECRET="your-kraken-api-secret"
```

---

## 3. Running the Monolith

### Start the Gateway

```bash
# Run with default config (config/streaming.yaml)
./bin/gateway

# Run with custom config
./bin/gateway --config=/path/to/custom.yaml

# Run with log level override
./bin/gateway --log-level=debug
```

**Expected Output**:

```
[INFO] Meltica Gateway starting...
[INFO] Loading configuration from config/streaming.yaml
[INFO] Initializing providers: binance, coinbase, kraken
[INFO] Connecting to binance WebSocket: wss://stream.binance.com:9443/ws
[INFO] Connecting to coinbase WebSocket: wss://ws-feed.exchange.coinbase.com
[INFO] Connecting to kraken WebSocket: wss://ws.kraken.com
[INFO] Fetching initial order book snapshots...
[INFO] Provider binance connected: 3 symbols subscribed
[INFO] Provider coinbase connected: 2 symbols subscribed
[INFO] Provider kraken connected: 2 symbols subscribed
[INFO] Orchestrator started: 2 merge rules configured
[INFO] Dispatcher started: routing version 1
[INFO] Consumers initialized: strategy-alpha (enabled), strategy-beta (disabled)
[INFO] Control Bus listening on port 8080
[INFO] Data Bus ready
[INFO] Telemetry endpoint: http://localhost:9090/metrics
[INFO] Gateway ready
```

### Verify Health

```bash
# Check health endpoint
curl http://localhost:8080/health

# Expected response:
{
  "status": "healthy",
  "providers": {
    "binance": "connected",
    "coinbase": "connected",
    "kraken": "connected"
  },
  "consumers": 2,
  "routing_version": 1
}
```

---

## 4. Interacting with the Monolith

### 4.1 Subscribe to Market Data

Send a control message to subscribe to market data:

```bash
curl -X POST http://localhost:8080/control/subscribe \
  -H "Content-Type: application/json" \
  -d '{
    "message_id": "sub-001",
    "consumer_id": "strategy-alpha",
    "type": "Subscribe",
    "payload": {
      "symbol": "BTC-USDT",
      "providers": ["binance"],
      "event_types": ["BookUpdate", "Trade"]
    },
    "timestamp": "2025-10-12T10:30:00Z"
  }'

# Expected response:
{
  "message_id": "sub-001",
  "consumer_id": "strategy-alpha",
  "success": true,
  "routing_version": 2,
  "timestamp": "2025-10-12T10:30:00.500Z"
}
```

### 4.2 Subscribe to Merged Data

Subscribe to merged multi-provider data:

```bash
curl -X POST http://localhost:8080/control/merged-subscribe \
  -H "Content-Type: application/json" \
  -d '{
    "message_id": "merge-001",
    "consumer_id": "strategy-alpha",
    "type": "MergedSubscribe",
    "payload": {
      "symbol": "ETH-USDT",
      "providers": ["binance", "coinbase"],
      "merge_config": {
        "window_duration": "10s",
        "max_events": 1000,
        "partial_policy": "suppress",
        "event_types": ["BookSnapshot"]
      }
    },
    "timestamp": "2025-10-12T10:30:00Z"
  }'
```

### 4.3 Enable Trading

Enable trading for a consumer (Trading Switch):

```bash
curl -X POST http://localhost:8080/control/set-trading-mode \
  -H "Content-Type: application/json" \
  -d '{
    "message_id": "trade-001",
    "consumer_id": "strategy-alpha",
    "type": "SetTradingMode",
    "payload": {
      "enabled": true
    },
    "timestamp": "2025-10-12T10:30:00Z"
  }'
```

### 4.4 Submit an Order

Submit a limit order (requires trading switch enabled):

```bash
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{
    "client_order_id": "client-order-ABC123",
    "consumer_id": "strategy-alpha",
    "provider": "binance",
    "symbol": "BTC-USDT",
    "side": "Buy",
    "order_type": "Limit",
    "price": "50000.00",
    "quantity": "0.10",
    "timestamp": "2025-10-12T10:30:00Z"
  }'

# Expected response:
{
  "client_order_id": "client-order-ABC123",
  "status": "submitted",
  "timestamp": "2025-10-12T10:30:00.200Z"
}
```

**Note**: If trading switch is disabled, the order will be suppressed locally:

```json
{
  "client_order_id": "client-order-ABC123",
  "status": "suppressed",
  "reason": "Trading switch disabled for consumer strategy-alpha",
  "timestamp": "2025-10-12T10:30:00.200Z"
}
```

### 4.5 Consume Events (WebSocket)

Consumers receive events via WebSocket connection to the Data Bus:

```javascript
// Example WebSocket client (JavaScript)
const ws = new WebSocket('ws://localhost:8080/databus/stream?consumer_id=strategy-alpha');

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('Received event:', data);
  
  switch (data.type) {
    case 'BookUpdate':
      console.log('Book update:', data.payload);
      break;
    case 'Trade':
      console.log('Trade:', data.payload);
      break;
    case 'ExecReport':
      console.log('Execution report:', data.payload);
      break;
  }
};

ws.onopen = () => console.log('Connected to Data Bus');
ws.onerror = (err) => console.error('WebSocket error:', err);
ws.onclose = () => console.log('Disconnected from Data Bus');
```

---

## 5. Monitoring and Telemetry

### 5.1 Prometheus Metrics

Metrics are exposed at `http://localhost:9090/metrics`:

```bash
curl http://localhost:9090/metrics
```

**Key Metrics**:
- `meltica_buffer_depth{stream_key}` - Current buffer depth per stream
- `meltica_coalesced_drops_total` - Total coalescable events dropped
- `meltica_throttled_milliseconds_total` - Total time spent throttled
- `meltica_suppressed_partials_total` - Total partial windows suppressed
- `meltica_book_resyncs_total{provider}` - Order book resync count per provider
- `meltica_checksum_failures_total{provider}` - Checksum failure count per provider
- `meltica_websocket_reconnects_total{provider}` - WebSocket reconnection count

### 5.2 Telemetry Events

Telemetry events are logged to `stdout` and optionally to a DLQ:

```bash
# Tail logs for telemetry
tail -f /var/log/meltica/telemetry.log

# Example telemetry event (book resync):
{
  "event_id": "telemetry-resync-001",
  "type": "book.resync",
  "severity": "WARN",
  "timestamp": "2025-10-12T10:30:05.000Z",
  "trace_id": "trace-abc123",
  "decision_id": "decision-resync-checksum",
  "metadata": {
    "provider": "binance",
    "symbol": "BTC-USDT",
    "reason": "checksum",
    "checksum_expected": "crc32:a1b2c3d4",
    "checksum_actual": "crc32:e5f6a7b8"
  }
}
```

### 5.3 Distributed Tracing

Distributed tracing is available via OpenTelemetry (Jaeger/Zipkin):

```bash
# Export traces to Jaeger
export OTEL_EXPORTER_JAEGER_ENDPOINT=http://localhost:14268/api/traces

# View traces in Jaeger UI
open http://localhost:16686
```

---

## 6. Testing

### Unit Tests

```bash
# Run all unit tests with race detector
make test
# or
go test ./... -race -count=1 -timeout=30s

# Run tests with coverage
go test ./... -race -coverprofile=coverage.out
go tool cover -html=coverage.out

# Coverage must be ≥70% to pass CI
```

### Integration Tests

Integration tests are gated under `//go:build integration`:

```bash
# Run integration tests
go test ./tests/integration/... -tags=integration -race -count=1 -timeout=2m

# Run specific integration test
go test ./tests/integration/end_to_end_test.go -tags=integration -v
```

### Test with Fake Providers

Use fake WebSocket/REST providers for local testing without network dependencies:

```bash
# Run with fake providers (no real exchange connections)
./bin/gateway --config=config/streaming.test.yaml --fake-providers
```

---

## 7. Common Scenarios

### Scenario 1: Consumer Receives Out-of-Order Events

**Observation**: Events arrive out-of-order from provider.

**System Behavior**:
1. Dispatcher buffers events in per-stream priority queue (sorted by `seq_provider`)
2. Flush timer (50ms) emits ordered events
3. Late events (>150ms) are dropped and logged to telemetry

**Verification**:
- Check telemetry for `late_event.dropped` events
- Verify `meltica_late_events_dropped_total` metric increments

### Scenario 2: Merge Window Suppression

**Observation**: Only 1 provider sends data; other provider is down.

**System Behavior**:
1. Orchestrator opens merge window on first event
2. Window closes after 10 seconds (timeout)
3. Only 1 of 2 expected providers present → suppress window (no emit)
4. Telemetry logs `merge.suppressed_partial` with missing provider

**Verification**:
- Check telemetry for `merge.suppressed_partial` event
- Verify consumer does NOT receive partial merged event

### Scenario 3: Order Book Checksum Failure

**Observation**: Provider sends order book update with invalid checksum.

**System Behavior**:
1. Book assembler applies WebSocket diff
2. Checksum verification fails
3. Discard corrupted book state
4. Trigger REST snapshot refresh
5. Telemetry logs `book.resync{reason=checksum}`

**Verification**:
- Check telemetry for `book.resync` event with `reason=checksum`
- Verify book recovers after REST snapshot fetch

### Scenario 4: Duplicate Order Detection

**Observation**: Consumer submits order with duplicate `client_order_id`.

**System Behavior**:
1. Dispatcher checks idempotency map
2. Duplicate detected → reject locally (no submission to provider)
3. Telemetry logs `order.duplicate_detected`
4. Return error response to consumer

**Verification**:
- Check telemetry for `order.duplicate_detected` event
- Verify order is NOT submitted to exchange

---

## 8. Troubleshooting

### Provider Connection Issues

**Symptom**: Provider shows "disconnected" status.

**Solutions**:
1. Check API credentials (environment variables)
2. Verify network connectivity to exchange
3. Check firewall rules (allow outbound WebSocket connections)
4. Review provider-specific rate limits

```bash
# Check provider connection status
curl http://localhost:8080/health | jq '.providers'
```

### High Latency

**Symptom**: Events have high end-to-end latency (>200ms).

**Solutions**:
1. Check system load (CPU, memory)
2. Review buffer depths in metrics (`meltica_buffer_depth`)
3. Reduce number of subscribed symbols
4. Increase `token_rate_per_stream` in config

### Partial Windows Not Suppressed

**Symptom**: Consumers receive partial merged events (missing providers).

**Solutions**:
1. Verify `partial_policy: suppress` in merge config
2. Check orchestrator logs for window closure events
3. Ensure all expected providers are connected

---

## 9. Next Steps

- **Read the Architecture**: [spec.md](./spec.md) for detailed requirements
- **Review Data Model**: [data-model.md](./data-model.md) for canonical event schemas
- **Explore Contracts**: [contracts/](./contracts/) for API schemas
- **Implement Strategies**: Write custom consumer strategies in `internal/consumer/`
- **Extend Providers**: Add new exchange providers in `internal/adapters/`

---

## 10. References

- **Specification**: [spec.md](./spec.md)
- **Implementation Plan**: [plan.md](./plan.md)
- **Research Decisions**: [research.md](./research.md)
- **Canonical Events Contract**: [contracts/canonical_events.yaml](./contracts/canonical_events.yaml)
- **Control Bus Contract**: [contracts/control_bus.yaml](./contracts/control_bus.yaml)
- **Telemetry Contract**: [contracts/telemetry.yaml](./contracts/telemetry.yaml)
- **Constitution**: [.specify/memory/constitution.md](../../.specify/memory/constitution.md)

---

**Happy Trading! 🚀**

