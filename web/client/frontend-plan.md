---
description: Analyze the user's input, then plan and act on frontend tasks using heuristic, sequential reasoning.
---

## User Input

```text
$ARGUMENTS
```

If user input is not empty, you **must** take it into account before proceeding.

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
