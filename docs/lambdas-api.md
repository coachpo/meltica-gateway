# Lambda Lifecycle HTTP API

The gateway exposes a REST surface under `/lambdas` for managing trading strategies at runtime. The handler is mounted alongside the existing control endpoints on port `:8880`.

## Collection Endpoints

### `GET /lambdas`
Returns the currently running lambda specifications.

```json
{
  "lambdas": [
    {
      "id": "lambda-fake-btc",
      "provider": "fake",
      "symbol": "BTC-USDT",
      "strategy": "noop",
      "config": {},
      "auto_start": true
    }
  ]
}
```

### `POST /lambdas`
Creates and starts a new lambda instance.

**Request**
```json
{
  "id": "lambda-fake-eth",
  "provider": "fake",
  "symbol": "ETH-USDT",
  "strategy": "logging",
  "config": {
    "logger_prefix": "[eth-strat] "
  }
}
```

**Responses**
- `201 Created` with the persisted spec on success.
- `400 Bad Request` if validation fails or the provider/strategy is unknown.

> `auto_start` is managed by the application; omit or set to `false` when creating lambdas at runtime.

## Item Endpoints

All item endpoints operate on `/lambdas/{id}`.

### `GET /lambdas/{id}`
Retrieves the stored specification for a running lambda.

### `PUT /lambdas/{id}`
Replaces the configuration for the lambda. The body mirrors the `POST` payload. The manager restarts the lambda with the new settings.

### `DELETE /lambdas/{id}`
Stops the lambda and removes it from the manager.

## Error Model

Errors are returned in a consistent JSON envelope:

```json
{
  "status": "error",
  "error": "lambda not found"
}
```

HTTP status codes communicate the class of error (`400` validation, `404` missing, `409` conflicts).

## Authentication & Transport

The API is served over HTTP without authentication by default. Deployments should place the gateway behind an ingress or service mesh that enforces TLS and access control as needed.
