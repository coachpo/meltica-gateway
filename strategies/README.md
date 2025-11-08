# Strategies Repository

This directory holds JavaScript strategy modules plus the helper tooling that keeps the
registry manifest in sync. When the strategies tree is split into its own repository,
this README becomes the entry point for contributors and operators.

## Layout

- `<strategy>/<tag>/<strategy>.js` — versioned strategy sources.
- `registry.json` — manifest consumed by the control plane.
- `bootstrap_strategies.py` — utility script for normalizing layouts, rebuilding the
  manifest, and auditing usage exports.

## Bootstrap Helper

```
python3 bootstrap_strategies.py \
  --root strategies \
  --usage http://localhost:8880/strategies/registry \
  --usage-output usage.json
```

Key flags:

- `--root`: strategy directory to scan (defaults to `strategies`).
- `--write`: move unversioned files into `<name>/<version>/<name>.js` before writing the
  registry.
- `--usage`: either a local JSON file or a URL; when provided the script prints a report
  of revisions with zero running instances.
- `--usage-output`: where to store the downloaded payload when `--usage` is a URL
  (defaults to `usage.json`).

The script requires Python 3.9+ and Node.js (used to evaluate each module’s exported
`metadata`).

## Typical Workflow

1. Export usage from the control plane (`GET /strategies/registry`) and pass it to the
   helper.
2. Review the printed zero-usage revisions, decommission them in the control plane, and
   rerun the helper with `--write` if the on-disk layout needs to match the requested
   versioning scheme.
3. Commit the updated `registry.json` alongside any strategy changes.
