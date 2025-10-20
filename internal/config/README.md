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
    Exchanges   map[Exchange]map[string]any   // Exchange-specific blobs
    Dispatcher  DispatcherConfig              // Routing configuration
    Eventbus    EventbusConfig                // Message bus sizing
    Pools       PoolConfig                    // Object pooling capacities
    APIServer   APIServerConfig               // Control server settings
    Telemetry   TelemetryConfig               // Observability settings
    ManifestPath string                       // Runtime manifest path
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

See `config/app.example.yaml` for a complete sample. Key sections include:

- `environment`: Deployment environment string (`dev`, `staging`, `prod`).
- `exchanges`: Arbitrary blobs forwarded to each exchange adapter.
- `dispatcher.routes`: Topic routing, REST polling, and filters.
- `eventbus`: In-memory event bus sizing.
- `pools`: Object pool capacities.
- `apiServer`: Control API bind address.
- `telemetry`: OTLP exporter configuration.
- `manifest`: Path to the runtime manifest (`config/runtime.yaml` by default).

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
6. **DRY Principle**: Exchanges define their details within their own packages

## File Organization

- `app_config.go` - Unified config structure, streaming types, and loading logic
- `types.go` - Shared config primitives (environment, exchange identifiers)
- `app_test.go` - Tests for unified config loading

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
