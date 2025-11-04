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

- **Ignore backward compatibility** — must always avoid writing shim code.
- If a modification is too large or complex, **must always stop immediately** and notify the user instead of attempting a partial workaround.
- **Must always retry** the command using **zsh** if it **fails** when run with **bash**.

- Must always use Capabilities MCP servers in the following cases:

  - `context7 MCP`:

    - Must always be used when up-to-date library/framework docs, API references, code samples, or migration notes are needed for the requested change.
    - Must always be used when resolving ambiguous library names or selecting between multiple packages.
    - Must always be used when external documentation is required to implement or verify behavior that is not obvious from the repository code.

  - `sequential-thinking MCP`:

    - Must always be used when requirements are ambiguous, multi-step, or involve trade-offs, to produce and verify a plan before acting.
    - Must always be used before large refactors, risky changes, or whenever branching alternatives need evaluation.
    - Must always be used when hypothesis generation or verification can reduce rework.

  - `playwright MCP`:

    - Must always be used when the user input includes a concrete, accessible target URL and you must reproduce a frontend bug, validate a flow, or capture evidence.
    - Must always be used when automating interactions in a browser for public URLs; never use it for localhost or private networks.

  - `shadcn MCP`:

    - Must always be used when searching for shadcn/ui components, usage examples, or when adding commands to scaffold UI parts requested by the user.
    - Must always be used when canonical usage patterns for components already in the project are required.

  - `figma MCP`:
    - Must always be used when the user explicitly provides a Figma file URL and requests reading specs, extracting assets, or syncing design tokens.
    - Must always be used when design details (spacing, typography, colors) are needed to implement a UI change.

- Must always prefer MCP servers whenever off-repo knowledge or external verification is needed; must always prefer local repo search tools for code that already exists in this repository.

## Validation

After implementation:

1. Run `make lint` to lint backend code.
2. Run `pnpm lint` to lint frontend code.
3. Run `make test` to test backend code.
