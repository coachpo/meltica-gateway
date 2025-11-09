# Scripts

This directory contains build, install, analysis, and utility scripts for the project.

## Purpose

- Build automation scripts
- Deployment scripts
- Database migration scripts
- Development utilities
- CI/CD helper scripts

## Usage

- `strategy-tags.sh`: Interactive helper for calling the control-plane tag APIs. It supports:
  - `assign <strategy> <tag> <hash>` to move aliases (defaults to `MELTICA_API=http://localhost:8880`).
  - `delete <strategy> <tag>` with optional `ALLOW_ORPHAN=true` to force removing the final alias.
  - Safety prompts show current/target hashes before calling the HTTP endpoints.
  - Requires `jq` for response formatting (`JQ_BIN` override supported).

Export `MELTICA_API` when targeting a remote control plane, e.g. `MELTICA_API=https://ctrl.meltica.local ./strategy-tags.sh assign meanreversion prod sha256:deadbeef`.
