# Strategy Versioning Rollout Playbook

This guide describes how to migrate an existing Meltica deployment from the legacy flat `strategies/` directory to the versioned registry model.

## Prerequisites

- Meltica binaries built after March 2025 (contain the registry-aware loader and HTTP upgrades).
- Access to the current strategy directory on every environment (dev, staging, prod).
- Operational window to restart the gateway (refreshing modules is enough, but enabling `requireRegistry` is simpler after a restart).

## 1. Audit Current Strategies

1. List existing `.js` strategy files.
2. Note the canonical names, active versions (if tracked externally), and which instances use each strategy.
3. Ensure all files export valid metadata with a `version` field.

## 2. Generate Versioned Layout & Registry

Run the bootstrap helper locally (or on the target host):

```bash
go run ./scripts/bootstrap_strategies \
  -root /path/to/strategies \
  -write
```

What this does:

- Computes the SHA-256 hash for each module.
- Moves flat files into `strategies/<name>/<version>/<name>.js` when needed.
- Emits/updates `strategies/registry.json` with tag → hash mappings.

Repeat the process for each environment. Version the new directory structure in source control or your artifact system if applicable.

## 3. Enable Registry Enforcement (Optional Initially)

Add the following to `config/app.yaml` once you are ready to enforce the new layout:

```yaml
strategies:
  directory: strategies
  requireRegistry: true
```

On startup the manager will now fail fast if `registry.json` is missing.

## 4. Deploy Updated Strategies

1. Copy the reorganized `strategies/` tree and `registry.json` to the target host.
2. Restart the gateway **or** invoke `POST /strategies/refresh` after copying.
3. Verify `/strategies/modules` shows the correct tags/hashes and `/strategy/instances` report the expected revision hashes.

## 5. Tag Promotion Workflow

- Upload new revisions via `POST /strategies/modules` with `{"tag":"v1.1.0","promoteLatest":true}` to atomically add the version and move `latest`.
- Use `aliases` to assign additional names (e.g., `stable`).
- Existing instances remain on their pinned hash until refreshed; schedule refreshes or redeploy them to roll forward.

## 6. Rollback Procedure

1. Identify the previous hash via `/strategies/modules` or the registry file.
2. `PUT /strategies/modules/<name>` with the older source + tag to promote it, or create a new instance using `name@hash`.
3. Refresh affected instances. Because hashes are immutable, reverting is a no-op if they already point to the desired revision.

## 7. Cleanup & Policy

- Deleting revisions (`DELETE /strategies/modules/<selector>`) is blocked while any instance pins the hash. Stop or reconfigure those instances first.
- Periodically prune unused tags/hashes from `registry.json` to avoid drift.
- Document your internal tag policy (e.g., `vMAJOR.MINOR.PATCH`, plus `latest`, `stable`).

## 8. Validation Checklist

- [ ] `/strategies/modules` returns all expected strategies with revision metadata.
- [ ] `/strategy/instances` shows the correct `strategyHash` for running lambdas.
- [ ] `POST /strategies/refresh` completes without errors.
- [ ] Backtest CLI runs with selector syntax (e.g., `-strategy delay:v1.0.0`).
- [ ] `requireRegistry: true` enabled (optional but recommended after migration).

## 9. Communication

- Notify operators and developers about the new selector syntax (`name`, `name:tag`, `name@hash`).
- Share this playbook and update runbooks with the upload/refresh/rollback commands.
- Schedule periodic reviews of registry contents to keep tags current.

With these steps complete, the deployment is fully version-aware: friendly tags for daily ops, immutable hashes for reproducibility, and documented procedures for promoting or rolling back strategy revisions.
