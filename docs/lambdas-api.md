# Strategy Management HTTP API

The gateway exposes REST endpoints for strategy discovery and instance lifecycle management. Handlers are mounted alongside control endpoints on port `:8880`.

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

## Strategy Instances

Instance resources are exposed under `/strategy-instances`.

### `GET /strategy-instances`
Lists all known instances (running or stopped).

```json
{
  "instances": [
    {
      "id": "lambda-fake-btc",
      "provider": "fake",
      "symbol": "BTC-USDT",
      "strategy": "noop",
      "config": {},
      "autoStart": true,
      "running": true
    }
  ]
}
```

### `POST /strategy-instances`
Creates and starts a new instance.

Request body (minimum fields):
```json
{
  "id": "lambda-fake-eth",
  "provider": "fake",
  "symbol": "ETH-USDT",
  "strategy": "logging",
  "config": { "logger_prefix": "[eth-strat] " }
}
```

Responses:
- `201 Created` with the created instance snapshot on success.
- `400 Bad Request` if validation fails or the provider/strategy is invalid/unavailable.

Note: `autoStart` in the request is ignored by the server; runtime-created instances are started immediately and returned as a snapshot.

## Instance Item Endpoints

All item endpoints operate on `/strategy-instances/{id}`.

### `GET /strategy-instances/{id}`
Returns the instance snapshot, or `404` if not found.

### `PUT /strategy-instances/{id}`
Replaces the configuration and restarts the instance. Provider, symbol, and strategy are immutable and cannot be changed; only `config` can be updated.

### `DELETE /strategy-instances/{id}`
Stops the instance (if running) and removes it from the manager. Returns:

```json
{ "status": "removed", "id": "<id>" }
```

### Actions

Action endpoints accept `POST` only:
- `POST /strategy-instances/{id}/start` – starts a stopped instance (`409` if already running)
- `POST /strategy-instances/{id}/stop` – stops a running instance (`409` if not running)

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

The API is served over HTTP without authentication by default. Place the gateway behind an ingress or service mesh that enforces TLS and access control as needed.
