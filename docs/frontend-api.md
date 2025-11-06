# Meltica Control Plane HTTP API

Base URL defaults to `http://localhost:8880`. All responses are UTF-8 JSON with permissive CORS. Request bodies are limited to 1 MiB; oversized payloads receive `413 Request Entity Too Large`. Errors are emitted as `{"status":"error","error":"message"}` with appropriate HTTP codes.

---

## Table of Contents

1. [Strategies](#strategies)
2. [Strategy Modules](#strategy-modules)
3. [Strategy Registry](#strategy-registry)
4. [Providers & Adapters](#providers--adapters)
5. [Instances & History](#instances--history)
6. [Orders, Executions, Balances](#orders-executions-balances)
7. [Risk Limits](#risk-limits)
8. [Context Backup & Restore](#context-backup--restore)
9. [Outbox](#outbox)
10. [Common Payloads](#common-payloads)
11. [Quality Gates](#quality-gates)

---

## Strategies

### `GET /strategies`
Returns every registered strategy.

```json
{
  "strategies": [
    {
      "name": "momentum",
      "displayName": "Momentum",
      "version": "1.3.0",
      "description": "...",
      "config": [{ "name": "lookback", "type": "duration", "...": "..." }],
      "events": ["ExecReport", "Ticker"]
    }
  ]
}
```

### `GET /strategies/{name}`
Looks up a strategy by name (case-insensitive). `404` when missing.

---

## Strategy Modules

### `GET /strategies/modules`
Query params:

| Param          | Type    | Notes                                    |
|----------------|---------|------------------------------------------|
| `strategy`     | string  | Filter by strategy name                  |
| `hash`         | string  | Filter by revision hash                  |
| `runningOnly`  | bool    | `true` limits to modules with live usage |
| `limit`        | int ≥0  | Defaults to no paging                    |
| `offset`       | int ≥0  | Defaults to 0                            |

Response:

```json
{
  "modules": [ { "name": "momentum", "file": "...", "hash": "...", "...": "..." } ],
  "total": 3,
  "offset": 0,
  "limit": 50,
  "strategyDirectory": "/abs/path/to/strategies"
}
```

### `POST /strategies/modules`
Create or update a JS module.

```json
{
  "filename": "momentum.js",
  "name": "momentum",
  "tag": "stable",
  "aliases": ["mom-v1"],
  "promoteLatest": true,
  "source": "// full JS source"
}
```

Response `201`:

```json
{
  "status": "pending_refresh",
  "strategyDirectory": "/workspace/strategies",
  "module": { "name": "momentum", "hash": "abc123", "tag": "stable", "version": "1.3.0", "file": "momentum.js", "path": "/workspace/strategies/momentum.js" }
}
```

### `GET /strategies/modules/{name}`
Fetch module summary (`js.ModuleSummary`).

### `PUT /strategies/modules/{name}`
Same payload as POST; returns updated resolution with `200 OK`.

### `DELETE /strategies/modules/{name}`
Removes the module (`204 No Content`).

### `GET /strategies/modules/{name}/source`
Returns `application/javascript` body of the module.

### `GET /strategies/modules/{name}/usage`
Query params: `includeStopped` (bool), `limit`, `offset`. Response:

```json
{
  "selector": "momentum:stable",
  "strategy": "momentum",
  "hash": "abc123",
  "usage": { "strategy": "momentum", "hash": "abc123", "instances": ["inst-1"], "count": 1, "firstSeen": "...", "lastSeen": "...", "running": true },
  "instances": [
    {
      "instanceSummary": { "id": "inst-1", "providers": ["..."], "...": "..." },
      "links": { "self": "/strategy/instances/inst-1", "usage": "/strategy/instances/inst-1/executions" }
    }
  ],
  "total": 1,
  "offset": 0,
  "limit": 10
}
```

---

## Strategy Registry

### `POST /strategies/refresh`
Body optional:
```json
{ "hashes": ["abc123"], "strategies": ["momentum:stable"] }
```
Empty body refreshes everything. Responses:
- Full refresh: `{"status":"refreshed"}`
- Targeted: `{"status":"partial_refresh","results":[{...}]}`.

### `GET /strategies/registry`
Returns registry manifest plus runtime usage:
```json
{
  "registry": {
    "momentum": {
      "tags": { "stable": "abc123" },
      "hashes": { "abc123": { "tag": "stable", "path": "/workspace/momentum.js" } }
    }
  },
  "usage": [
    { "strategy": "momentum", "hash": "abc123", "instances": ["inst-1"], "count": 1, "firstSeen": "...", "lastSeen": "...", "running": true }
  ]
}
```

---

## Providers & Adapters

### `GET /providers`
```json
{
  "providers": [
    {
      "name": "binance-spot",
      "adapter": "binance",
      "identifier": "binance",
      "instrumentCount": 120,
      "settings": { "...": "..." },
      "running": true,
      "status": "running",
      "startupError": "",
      "dependentInstances": ["arb-eur"],
      "dependentInstanceCount": 1
    }
  ]
}
```

### `POST /providers`
```json
{
  "name": "binance-spot",
  "adapter": {
    "identifier": "binance",
    "config": { "apiKey": "abc", "apiSecret": "xyz" }
  },
  "enabled": true
}
```
Response `202 Accepted` with `provider.RuntimeDetail` and `Location: /providers/{name}`. `enabled=true` triggers async start.

### `GET /providers/{name}`
Detailed runtime metadata with instrument catalog and adapter schema.

### `PUT /providers/{name}`
Same payload as POST. Body `name` must match path (case-insensitive). Response `200`.

### `DELETE /providers/{name}`
Fails with `409` if any instances depend on the provider. Success payload `{"status":"removed","name":"..."}`.

### Provider Actions
- `POST /providers/{name}/start` → async start (202).
- `POST /providers/{name}/stop` → stop instance (200).
- `GET /providers/{name}/balances` → see [Orders, Executions, Balances](#orders-executions-balances).

### `GET /adapters`
Lists adapter metadata (identifier, displayName, venue, capability list, `settingsSchema` definitions).

### `GET /adapters/{identifier}`
Fetch single adapter metadata, `404` if unknown.

---

## Instances & History

### `GET /strategy/instances`
```json
{
  "instances": [
    {
      "instanceSummary": {
        "id": "arb-eur",
        "strategyIdentifier": "momentum",
        "strategyTag": "stable",
        "strategyHash": "abc123",
        "strategyVersion": "1.3.0",
        "strategySelector": "momentum:stable",
        "providers": ["binance-spot","okx-spot"],
        "aggregatedSymbols": ["BTCEUR","ETHEUR"],
        "running": true,
        "usage": { "...": "..." }
      },
      "links": { "self": "/strategy/instances/arb-eur", "usage": "/strategies/modules/momentum/usage" }
    }
  ]
}
```

### `POST /strategy/instances`
```json
{
  "id": "arb-eur",
  "strategy": { "identifier": "momentum", "config": { "lookback": "5m" } },
  "scope": {
    "binance-spot": { "symbols": ["BTCEUR","ETHEUR"] },
    "okx-spot": { "symbols": ["BTCEUR"] }
  }
}
```
Response `201` with snapshot, including inferred selector/tag/hash/version fields and provider list.

### `GET /strategy/instances/{id}`
Returns snapshot plus hypermedia links.

### `PUT /strategy/instances/{id}`
Same payload as POST. Body `id` must match path (or be omitted). Response `200`.

### `DELETE /strategy/instances/{id}`
Returns `{"status":"removed","id":"..."}`.

### Instance Actions
- `POST /strategy/instances/{id}/start` or `/stop` → `{"status":"ok","id":"...","action":"start|stop"}`.
- `GET /strategy/instances/{id}/orders`
  - Query params: `limit` (default 50), multiple `state=ACK`, `provider`.
  - Response `{"orders": orderstore.OrderRecord[], "count": n}`.
- `GET /strategy/instances/{id}/executions`
  - Query: `limit` (default 100), `provider`, `orderId`.
  - Response `{"executions": orderstore.ExecutionRecord[], "count": n}`.

---

## Orders, Executions, Balances

Schemas are provided by `internal/domain/orderstore/store.go`.

- **OrderRecord**: base order fields plus `acknowledgedAt`, `completedAt`, `createdAt`, `updatedAt`. Metadata keys use camelCase (e.g., `exchangeOrderId`, `filledQuantity`, `remainingQty`, `avgFillPrice`, `rejectReason`, `commissionAmount`, `commissionAsset`).
- **ExecutionRecord**: includes `fee`, `feeAsset`, `liquidity`, `tradedAt`, metadata (e.g., `remainingQty`, `state`, `eventSymbol`).
- **BalanceRecord**: per-provider asset totals with `snapshotAt`, `createdAt`, `updatedAt`.

### `GET /providers/{name}/balances`
Query: `limit` (default 100, max 500), `asset`. Response `{"balances": BalanceRecord[], "count": n}`.

---

## Risk Limits

### `GET /risk/limits`
Returns current `config.RiskConfig`:

```json
{
  "limits": {
    "maxPositionSize": "10",
    "maxNotionalValue": "500000",
    "notionalCurrency": "USD",
    "orderThrottle": 5,
    "orderBurst": 3,
    "maxConcurrentOrders": 50,
    "priceBandPercent": 1.5,
    "allowedOrderTypes": ["limit","market"],
    "killSwitchEnabled": true,
    "maxRiskBreaches": 3,
    "circuitBreaker": { "threshold": 5, "cooldown": "5m", "enabled": true }
  }
}
```

### `PUT /risk/limits`
Accepts the same structure; blanks trimmed, numeric defaults applied, allowed order types normalized to lowercase. Response `{"status":"updated","limits":{...}}`.

---

## Context Backup & Restore

### `GET /context/backup`
Snapshot:

```json
{
  "providers": [ { "name": "binance-spot", "adapter": "binance", "config": { "...": "..." } } ],
  "lambdas": [ { "id": "arb-eur", "strategy": { "...": "..." }, "scope": { "...": "..." } } ],
  "risk": { "...": "..." }
}
```

### `POST /context/backup`
Reapplies providers, lambdas, and risk config. Response `{"status":"restored"}`.

---

## Outbox

### `GET /outbox`
Query `limit` (default 100, max 500). Response:

```json
{
  "events": [
    {
      "id": 42,
      "aggregateType": "provider",
      "aggregateID": "binance-spot",
      "eventType": "Trade",
      "payload": { "eventId": "evt-1", "instrument": "BTCEUR" },
      "headers": { "provider": "binance-spot", "symbol": "BTCEUR", "eventId": "evt-1" },
      "availableAt": "2024-06-01T12:00:00Z",
      "publishedAt": null,
      "attempts": 0,
      "lastError": "",
      "delivered": false,
      "createdAt": "2024-06-01T12:00:00Z"
    }
  ],
  "count": 1
}
```

### `DELETE /outbox/{id}`
Deletes an entry. Response `{"id":42,"status":"deleted"}`.

---

## Common Payloads

- **Strategy Metadata** (`strategies.Metadata`): `name`, `displayName`, `version`, `description`, `config[]`, `events[]`.
- **Module Summary** (`js.ModuleSummary`): file/path/hash/version/tags, alias maps, revision history, running usage, strategy metadata.
- **Provider Metadata** (`provider.RuntimeMetadata`): adapter identifiers, instrumentation counts, runtime status, dependent instances.
- **Provider Detail** (`provider.RuntimeDetail`): `RuntimeMetadata` + `schema.Instrument[]` + `provider.AdapterMetadata`.
- **Lambda Spec** (`config.LambdaSpec`): `id`, strategy block (identifier/config/selector/tag/hash/version), scope map.
- **Risk Config**: see [Risk Limits](#risk-limits).
- **Orderstore Records**: described above.
- **Outbox Record** (`outboxstore.EventRecord`): aggregate identifiers, event payload/headers, timing metadata, attempts, delivery status.

---

## Quality Gates

Run before shipping API changes:

```bash
make lint   # golangci-lint run --config .golangci.yml
make test   # go test ./... -race -count=1 -timeout=30s
```

Both currently pass (see latest logs).

---
