# Unified Configuration Management

## Overview

The Meltica configuration system provides a single, unified entry point for all application configuration sourced directly from YAML.

## Architecture

### Single Config Entry Point

All configuration is managed through `AppConfig` in `app_config.go`:

```go
cfg, err := config.Load(ctx, "config/app.yaml")
```

### Configuration Structure

```go
type AppConfig struct {
    Environment Environment                   // dev, staging, prod
    Providers   map[Exchange]map[string]any   // Provider-specific blobs
    Eventbus    EventbusConfig                // Message bus sizing
    Pools       PoolConfig                    // Object pooling capacities
    APIServer   APIServerConfig               // Control server settings
    Telemetry   TelemetryConfig               // Observability settings
    LambdaManifest LambdaManifest             // Inline lambda definitions
}
```

## Usage

### Basic Loading

```go
import "github.com/coachpo/meltica/internal/infra/config"

func main() {
    ctx := context.Background()
    cfg, err := config.Load(ctx, "config/app.yaml")
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Environment: %s", cfg.Environment)
    log.Printf("Providers: %d", len(cfg.Providers))
}
```

### YAML Configuration

See `config/app.example.yaml` for a complete sample. Key sections include:

- `environment`: Deployment environment string (`dev`, `staging`, `prod`).
- `providers`: Arbitrary blobs forwarded to each exchange adapter; each entry must include an `exchange` block referencing a registered provider type (aliases map to the same exchange by using the same `exchange.name` value).
- `eventbus`: In-memory event bus sizing.
- `pools`: Object pool capacities.
- `apiServer`: Control API bind address.
- `telemetry`: OTLP exporter configuration.
- `lambdaManifest`: Inline lambda definitions materialised at startup.

## Migration from Old System

### Before (Fragmented)

Legacy setups required stitching together `config.FromEnv()` with bespoke streaming YAML readers scattered across the codebase. Each subsystem (dispatcher, telemetry, adapters) pulled settings from different structs, forcing manual reconciliation.

### After (Unified)

```go
// Single source of truth
cfg, _ := config.Load(ctx, path)

// Everything in one place
bin, _ := cfg.Providers["binance"]
telemetry := cfg.Telemetry
```

## Benefits

1. **Single Source of Truth**: One config structure for all concerns
2. **Deterministic Input**: Configuration comes entirely from YAML
3. **Type Safety**: Compile-time validation of config structure
4. **Easier Testing**: Mock entire config or individual sections
5. **Better Validation**: Validate all settings together in one pass
6. **DRY Principle**: Providers define their details within their own packages

## File Organization

- `app_config.go` - Unified config structure, YAML loading, and validation
- `types.go` - Shared config primitives (environment, exchange identifiers)
- `app_config_test.go` - Tests for unified config loading

## Testing

```bash
# Test unified config
go test ./internal/infra/config -run TestLoad

# Test all config functionality
go test ./internal/infra/config/...
```
