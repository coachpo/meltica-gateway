---
description: Analyze the user's input, then plan and act on frontend tasks using heuristic, sequential reasoning.
---

## User Input - If this section is not empty, you **must** take it into account before proceeding.

Frontend Update Plan

API Contracts – sync with persistence-backed endpoints (/strategy/instances, /providers/\*, /orders, /executions, /balances), confirm pagination/filter params, and delete all UI code that still reads the legacy lambdaManifest.
TypeScript Models & Client – expand LambdaInstance models (baseline/dynamic, timestamps, hashes), add typed records for orders/executions/balances, refresh context-backup payloads, and regenerate API hooks so everything flows through the JSON contracts.
UI Experiences – rework λ instance list/detail to surface new metadata plus tabs for order/execution/balance history, move creation/editing flows to JSON forms, and add provider guardrails (baseline vs dynamic visibility, delete restrictions, dependency warnings).
Context Backup/Restore – replace manifest upload/download widgets with JSON-only workflows, add schema validation and helper copy explaining how persisted state is restored, and ensure exports include all instances/operators require.
State Management & Routing – retire manifest caches, ensure hooks poll the DB-backed APIs, introduce routes for the new history tabs, and gate baseline instances from mutation paths.
Testing & Rollout – update unit/E2E coverage, add regression cases for baseline/dynamic behavior plus history rendering, coordinate deployment so backend persistence ships first, and document the new flows for operators (include change notes in web/client/frontend-plan.md).

## Capabilities

- `context7 MCP` — search frontend-related documentation, frameworks, and API references.
- `sequential-thinking MCP` — plan frontend actions heuristically and step-by-step.
- `playwright MCP` — interact with a frontend only if the user input includes the target URL.
- `shadcn MCP` — search and scaffold shadcn/ui components.
- `figma MCP` — read provided Figma links when requested.

## Planning and Execution

- **Ignore backward compatibility** — must always avoid writing shim code.
- If a modification is too large or complex, **must stop immediately** and notify the user instead of attempting a partial workaround.
- **Must retry** the command using **zsh** if it **fails** when run with **bash**.

- Must always use Capabilities MCP servers in the following cases:

  - `context7 MCP`:

    - Must always be used for up-to-date frontend framework docs, component libraries, or migration guides.
    - Must always be used when resolving ambiguous UI package names.
    - Must always be used when verifying external component or API behavior.

  - `sequential-thinking MCP`:

    - Must always be used before large UI refactors or flow redesigns.
    - Must always be used when frontend requirements are multi-step or trade-off driven.

  - `playwright MCP`:

    - Must always be used for reproducing frontend bugs, validating flows, or capturing screenshots of public URLs.

  - `shadcn MCP`:

    - Must always be used to locate or scaffold canonical shadcn/ui components and usage examples.

  - `figma MCP`:
    - Must always be used when a Figma file URL is provided for syncing design specs or extracting visual details.

## Validation

After implementation:

1. Run `pnpm lint` to lint frontend code.
2. Run `pnpm test` (or equivalent) to test frontend behavior.
