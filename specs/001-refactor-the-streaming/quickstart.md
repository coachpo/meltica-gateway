# Quickstart: Dispatcher-Conductor Streaming Stack

## 1. Prerequisites
- Go 1.25 installed (`go version` should report 1.25.x).
- Access to Binance public endpoints (WS + REST) or local fakes for integration tests.
- Optional: OpenTelemetry collector reachable via OTLP/HTTP (default `http://localhost:4318`).

## 2. Configure the pipeline
1. Copy the sample config (`config/streaming.example.yaml`, to be added) to your workspace and edit as needed. Key fields:
   ```yaml
   adapter:
     binance:
       ws:
         publicUrl: wss://stream.binance.com:9443/stream
         handshakeTimeout: 10s
       rest:
         orderbookSnapshot:
           interval: 5s
           endpoint: /api/v3/depth
   dispatcher:
     routes:
       TICKER:
         wsTopics: ["ticker.BTCUSDT", "ticker.ETHUSDT"]
         restFns: []
         filters:
           - field: instrument
             op: eq
             value: BTC-USDT
   snapshot:
     ttl: 15m
   databus:
     bufferSize: 1024
   telemetry:
     otlpEndpoint: http://localhost:4318
     serviceName: meltica-gateway
   ```
2. Export overrides via environment variables when necessary:
   ```bash
   export MELTICA_CONFIG=./config/streaming.yaml
   export BINANCE_WS_PUBLIC_URL=wss://stream.binance.com:9443/stream
   export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
   ```

## 3. Build and run the gateway
```bash
go mod tidy
make build
./bin/gateway --config ${MELTICA_CONFIG}
```
The gateway wires the Binance adapter, dispatcher, conductor, buses, and snapshot store. It logs OTLP trace initialization and begins consuming Binance streams.

## 4. Manage subscriptions via Control Bus
Use the REST facade defined in `contracts/controlbus.yaml`:
```bash
curl -X POST https://api.meltica.local/control/subscribe   -H 'Content-Type: application/json'   -d '{
        "type": "TICKER",
        "filters": {"instrument": "BTC-USDT"},
        "traceId": "demo-trace"
      }'
```
Expect a 202 response within 2 seconds. To cancel:
```bash
curl -X POST https://api.meltica.local/control/unsubscribe   -H 'Content-Type: application/json'   -d '{"type": "TICKER"}'
```

## 5. Verify data flow
1. Watch the Data Bus stub logs for published `MelticaEvent` payloads.
2. Run integration smoke tests with fakes:
   ```bash
   go test ./tests/integration -run Smoke -race
   ```
3. Inspect OpenTelemetry traces with your collector/UI to ensure spans link Adapter → Dispatcher → Conductor → Data Bus.

## 6. Troubleshooting
- **No events**: confirm dispatcher routes include the canonical type and that the control bus ack shows `accepted`.
- **REST poller drift**: adjust `dispatcher.routes[*].restFns[*].interval` in configuration.
- **Snapshot conflicts**: ensure Conductor retries CAS with refreshed snapshot before failing.
- **Telemetry disabled**: set `telemetry.otlpEndpoint` empty to fall back to the no-op provider.
