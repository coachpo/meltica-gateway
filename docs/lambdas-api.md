# Control API

The gateway exposes REST endpoints for strategy discovery, provider metadata, adapter definitions, and instance lifecycle management. Handlers are mounted alongside control endpoints on port `:8880`.

## Strategy Catalog

### `GET /strategies`
Returns available strategy definitions and their configurable fields.

```json
{
  "strategies": [
    {
      "name": "logging",
      "displayName": "Logging",
      "description": "Emits detailed logs for all inbound events.",
      "config": [
        { "name": "logger_prefix", "type": "string", "description": "Prefix prepended to each log message", "default": "[Logging] ", "required": false }
      ],
      "events": ["Trade", "Ticker", "BookSnapshot", "ExecReport", "KlineSummary", "BalanceUpdate"]
    }
  ]
}
```

### `GET /strategies/{name}`
Returns the metadata for a single strategy.

## Providers Metadata

### `GET /providers`
Lists all running providers with runtime metadata.

```json
{
  "providers": [
    {
      "name": "binance-spot",
      "exchange": "binance",
      "identifier": "binance",
      "instrumentCount": 342,
      "settings": {
        "api_key": "${BINANCE_API_KEY}",
        "api_secret": "${BINANCE_API_SECRET}",
        "snapshot_depth": 1000,
        "http_timeout": "10s",
        "instrument_refresh_interval": "30m",
        "recv_window": "5s",
        "user_stream_keepalive": "15m"
      }
    }
  ]
}
```

### `GET /providers/{name}`
Returns the detailed metadata, including the current instrument catalogue and underlying adapter definition.

```json
{
  "name": "binance-spot",
  "exchange": "binance",
  "identifier": "binance",
  "instrumentCount": 342,
  "settings": {
    "snapshot_depth": 1000,
    "http_timeout": "10s"
  },
  "instruments": [
    {
      "symbol": "BTC-USDT",
      "baseCurrency": "BTC",
      "quoteCurrency": "USDT",
      "pricePrecision": 2,
      "quantityPrecision": 6
    }
  ],
  "adapter": {
    "identifier": "binance",
    "displayName": "Binance Spot",
    "venue": "BINANCE",
    "capabilities": ["market-data", "orders"],
    "settingsSchema": [
      {"name": "snapshot_depth", "type": "int", "default": 1000, "required": false},
      {"name": "http_timeout", "type": "duration", "default": "10s", "required": false}
    ]
  }
}
```

## Strategy Instances

Instance resources are exposed under `/strategy/instances`.

### `GET /strategy/instances`
Lists all known instances (running or stopped) using a flattened summary payload.

```json
{
  "instances": [
    {
      "id": "latency-probe-btc",
      "strategyIdentifier": "logging",
      "providers": [
        "binance-spot"
      ],
      "aggregatedSymbols": [
        "BTC-USDT"
      ],
      "running": true
    }
  ]
}
```

Each instance summary includes:

- `strategyIdentifier` – name of the registered strategy.
- `providers` – normalized list of providers derived from the scope mapping.
- `aggregatedSymbols` – deduplicated union of symbols across all providers.
- `running` – current execution state.

### `POST /strategy/instances`
Creates a new instance. Instances are persisted in a stopped state; start them explicitly with `POST /strategy/instances/{id}/start`.

Request body (minimum fields):
```json
{
  "id": "latency-probe-eth",
  "strategy": {
    "identifier": "logging",
    "config": {
      "logger_prefix": "[LatencyProbe] ",
      "dry_run": true
    }
  },
  "scope": {
    "binance-spot": {
      "symbols": ["ETH-USDT"]
    }
  }
}
```

Responses:
- `201 Created` with the created instance snapshot on success.
- `400 Bad Request` if validation fails or the provider/strategy is invalid/unavailable.

Notes:
- `scope` must supply at least one provider with at least one symbol; providers are inferred from this map.
- `POST /strategy/instances/{id}/start` and `/stop` control runtime execution of a saved instance.

## Instance Item Endpoints

All item endpoints operate on `/strategy/instances/{id}`.

### `GET /strategy/instances/{id}`
Returns the detailed instance snapshot, including strategy configuration and provider scope, or `404` if not found.

```json
{
  "id": "latency-probe-btc",
  "strategy": {
    "identifier": "logging",
    "config": {
      "dry_run": true,
      "logger_prefix": "[LatencyProbe] "
    }
  },
  "providers": [
    "binance-spot"
  ],
  "scope": {
    "binance-spot": {
      "symbols": [
        "BTC-USDT"
      ]
    }
  },
  "aggregatedSymbols": [
    "BTC-USDT"
  ],
  "running": true
}
```

### `PUT /strategy/instances/{id}`
Replaces the configuration and restarts the instance. Provider, symbol, and strategy are immutable and cannot be changed; only `config` can be updated.

### `DELETE /strategy/instances/{id}`
Stops the instance (if running) and removes it from the manager. Returns:

```json
{ "status": "removed", "id": "<id>" }
```

### Actions

Action endpoints accept `POST` only:
- `POST /strategy/instances/{id}/start` – starts a stopped instance (`409` if already running)
- `POST /strategy/instances/{id}/stop` – stops a running instance (`409` if not running)

## Error Model

Errors use a consistent JSON envelope:

```json
{ "status": "error", "error": "strategy instance not found" }
```

Status codes:
- `400` validation or generic errors
- `404` resource not found
- `409` conflict (exists, already running, not running)
- `405` method not allowed

## Authentication & Transport

## Adapters Metadata

### `GET /adapters`
Returns static metadata for all registered adapters, including supported configuration keys.

```json
{
  "adapters": [
    {
      "identifier": "binance",
      "displayName": "Binance Spot",
      "venue": "BINANCE",
      "capabilities": ["market-data", "orders"],
      "settingsSchema": [
        {"name": "api_key", "type": "string", "required": false},
        {"name": "api_secret", "type": "string", "required": false},
        {"name": "snapshot_depth", "type": "int", "default": 1000, "required": false}
      ]
    }
  ]
}
```

### `GET /adapters/{identifier}`
Returns the metadata for a single adapter.

The API is served over HTTP without authentication by default. Place the gateway behind an ingress or service mesh that enforces TLS and access control as needed.
