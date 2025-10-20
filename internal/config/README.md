# Unified Configuration Management

## Overview

The Meltica configuration system provides a single, unified entry point for all application configuration with clear precedence rules.

## Architecture

### Single Config Entry Point

All configuration is managed through `AppConfig` in `app_config.go`:

```go
cfg, err := config.Load(ctx, "config/app.yaml")
```

### Configuration Precedence

Settings are applied in order (later overrides earlier):

1. **Code Defaults** (`app_config.go`)
2. **YAML File** (`app.yaml`)
3. **Environment Variables**

### Configuration Structure

```go
type AppConfig struct {
    Environment Environment                   // dev, staging, prod
    Exchanges   map[Exchange]ExchangeSettings // Exchange connection settings
    Adapters    AdapterSet                    // Adapter-specific config
    Dispatcher  DispatcherConfig              // Routing configuration
    Event Bus     Event BusConfig                 // Message bus sizing
    Telemetry   TelemetryConfig               // Observability settings
}
```

## Usage

### Basic Loading

```go
import "github.com/coachpo/meltica/internal/config"

func main() {
    ctx := context.Background()
    cfg, err := config.Load(ctx, "config/app.yaml")
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Environment: %s", cfg.Environment)
    log.Printf("Exchanges: %d", len(cfg.Exchanges))
}
```

### Environment Variable Overrides

```bash
# Override environment
export MELTICA_ENV=dev

# Override telemetry
export OTEL_SERVICE_NAME=my-service
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
```

### YAML Configuration

```yaml
environment: prod

# Exchange connection settings (optional, has code defaults)
exchanges:
  binance:
    rest:
      spot: https://api.binance.com
    websocket:
      publicUrl: wss://stream.binance.com:9443/stream
    http_timeout: 10s

# Adapter settings
adapter:
  binance:
    ws:
      publicUrl: wss://stream.binance.com:9443/stream
      handshakeTimeout: 10s
    rest:
      baseUrl: https://api.binance.com
      snapshot:
        endpoint: /api/v3/depth
        interval: 5s
        limit: 100
    book_refresh_interval: 1m

# Dispatcher routing
dispatcher:
  routes:
    TICKER:
      wsTopics:
        - ticker.BTCUSDT
      filters:
        - field: instrument
          op: eq
          value: BTC-USDT

# Data bus
eventbus:
  bufferSize: 1024

# Telemetry
telemetry:
  otlpEndpoint: http://localhost:4318
  serviceName: meltica-gateway
  enableMetrics: true
```

## Migration from Old System

### Before (Fragmented)

Legacy setups required stitching together `config.FromEnv()` with bespoke streaming YAML readers scattered across the codebase. Each subsystem (dispatcher, telemetry, adapters) pulled settings from different structs, forcing manual reconciliation.

### After (Unified)

```go
// Single source of truth
cfg, _ := config.Load(ctx, path)

// Everything in one place
bin, _ := cfg.Exchanges["binance"]
routes := cfg.Dispatcher.Routes
telemetry := cfg.Telemetry
```

## Benefits

1. **Single Source of Truth**: One config structure for all concerns
2. **Clear Precedence**: Defaults → YAML → Env vars
3. **Type Safety**: Compile-time validation of config structure
4. **Easier Testing**: Mock entire config or individual sections
5. **Better Validation**: Validate all settings together in one pass
6. **DRY Principle**: No duplication between `exchange_settings.go` and `app.yaml`

## File Organization

- `app_config.go` - Unified config structure, streaming types, and loading logic
- `exchange_settings.go` - Exchange settings types (reused by app_config.go)
- `app_test.go` - Tests for unified config loading
- `config_test.go` - Tests for core exchange configuration helpers

## Testing

```bash
# Test unified config
go test ./internal/config -run TestLoad

# Test all config functionality
go test ./internal/config/...
```

## Environment Variables Reference

| Variable | Description | Example |
|----------|-------------|---------|
| `MELTICA_ENV` | Runtime environment | `dev`, `staging`, `prod` |
| `MELTICA_CONFIG` | Config file path | `config/app.yaml` |
| `OTEL_SERVICE_NAME` | Telemetry service name | `meltica-gateway` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP endpoint | `http://localhost:4318` |
