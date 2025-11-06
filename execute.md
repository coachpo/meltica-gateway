---
description: Analyze the user's input, then plan and act on backend tasks using heuristic, sequential reasoning.
---

## User Input

```text
$ARGUMENTS
```

If user input is not empty, you **must** take it into account before proceeding.

## Capabilities

- `context7 MCP` — search backend-related documentation, frameworks, and API references.
- `sequential-thinking MCP` — plan backend actions heuristically and step-by-step.

## Planning and Execution

- **Ignore backward compatibility** — must always avoid writing shim code.
- If a modification is too large or complex, **must stop immediately** and notify the user instead of attempting a partial workaround.
- **Must retry** the command using **zsh** if it **fails** when run with **bash**.

- Must always use Capabilities MCP servers in the following cases:

  - `context7 MCP`:
    - Must always be used when consulting API docs, backend libraries, or migration notes.
    - Must always be used to verify framework behaviors or database API semantics.

  - `sequential-thinking MCP`:
    - Must always be used for multi-step backend tasks, refactors, or trade-off evaluations.
    - Must always be used for hypothesis generation and verification to reduce rework.

## Validation

After implementation:

1. Run `make lint` to lint backend code.
2. Run `make test` to test backend code.
