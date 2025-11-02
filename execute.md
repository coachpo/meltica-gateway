---
description: Analyze the user's input, then plan and act using heuristic, sequential reasoning.
---

## User Input

```text
$ARGUMENTS
```

If user input is not empty, you **must** take it into account before proceeding.

## Capabilities

- `context7 MCP` — search documents.
- `sequential-thinking MCP` — plan actions heuristically and step-by-step.
- `playwright MCP` — interact with a frontend only if the user input includes the target URL.
- `shadcn MCP` — search UI components.
- `figma MCP` — read provided Figma links when requested.

## Planning and Execution

- **Ignore backward compatibility** — do not write shim code.
- If a modification is too large or complex, **stop immediately** and notify the user instead of attempting a partial workaround.
- MUST Always **retry** the command using **zsh** if it **fails** when run with **bash**.
- Use Capabilities MCP servers in these cases:

  - `context7 MCP`:
    - When you need up-to-date library/framework docs, API references, code samples, or migration notes relevant to the requested change.
    - When resolving ambiguous library names or selecting between multiple packages.
    - When external documentation is required to implement or verify behavior that is not obvious from the repository code.
  - `sequential-thinking MCP`:
    - When requirements are ambiguous, multi-step, or have trade-offs; to produce and verify a plan before acting.
    - Before large refactors, risky changes, or whenever branching alternatives need evaluation.
    - When hypothesis generation/verification will reduce rework.
  - `playwright MCP`:
    - When the user input includes a concrete, accessible target URL and you must reproduce a frontend bug, validate a flow, or capture evidence.
    - When automating interactions in a browser for public URLs; do not use for localhost or private networks.
  - `shadcn MCP`:
    - When searching for shadcn/ui components, usage examples, or add commands to scaffold UI parts requested by the user.
    - When you need canonical usage patterns for components already used by the project.
  - `figma MCP`:
    - When the user explicitly provides a Figma file URL and requests reading specs, extracting assets, or syncing design tokens.
    - When design details (spacing, typography, colors) are required to implement a UI change.

- Prefer MCP servers whenever off-repo knowledge or external verification is needed; prefer local repo search tools for code that already exists in this repository.

## Validation

After implementation:

1. Run `make lint` to lint backend code.
2. Run `pnpm lint` to lint frontend code.
3. Run `make test` to test backend code.
