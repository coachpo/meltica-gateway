# Strategies Repository

This directory holds JavaScript strategy modules plus the helper tooling that keeps the
registry manifest in sync. When the strategies tree is split into its own repository,
this README becomes the entry point for contributors and operators.

## Layout

- `<strategy>/<tag>/<strategy>.js` — versioned strategy sources.
- `registry.json` — manifest consumed by the control plane.
- `bootstrape.py` — minimal helper for onboarding strategies (registry rebuilds and
  optional layout normalization).

## Bootstrap Helper

```
python3 bootstrape.py
```

The helper runs a single onboarding flow: point it at the strategies root, decide
whether to normalize unversioned files before writing `registry.json`, confirm, and it
will rebuild the manifest. Any response of “no” to the confirmation aborts the run. The
script requires Python 3.9+ and Node.js (used to evaluate each module’s exported
`metadata`).

## Typical Workflow

1. Export usage from the control plane (`GET /strategies/registry`) and pass it to the
   helper.
2. Review the printed zero-usage revisions, decommission them in the control plane, and
   rerun the helper with `--write` if the on-disk layout needs to match the requested
   versioning scheme.
3. Commit the updated `registry.json` alongside any strategy changes.
