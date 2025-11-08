# Strategies Repository

This directory holds JavaScript strategy modules plus the helper tooling that keeps the
registry manifest in sync. When the strategies tree is split into its own repository,
this README becomes the entry point for contributors and operators.

## Layout

- `<strategy>/<tag>/<strategy>.js` — versioned strategy sources.
- `registry.json` — manifest consumed by the control plane.

## Typical Workflow

1. Export usage from the control plane (`GET /strategies/registry`) and pass it to the
   helper.
2. Review the printed zero-usage revisions, decommission them in the control plane, and
   rerun the helper with `--write` if the on-disk layout needs to match the requested
   versioning scheme.
3. Commit the updated `registry.json` alongside any strategy changes.
