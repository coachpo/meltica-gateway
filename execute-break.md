---
description: Analyze the user's input, then plan and act using heuristic, sequential reasoning.
---

## User Input

```text

### Strategy Refactoring and Runtime Management Plan

**Current Status:**
All trading strategies are currently implemented in Golang.

**Objective:**
Refactor existing strategies into **JavaScript** using the **Goja** framework, enabling dynamic strategy management and hot updates at runtime.

---

### Backend Requirements

1. **Strategy Migration** internal/app/lambda/strategies
   - Convert existing Golang strategies into JavaScript modules compatible with Goja.
   - The JavaScript version must preserve the **same lifecycle**, **behaviors**, and **memory model** as the original Golang implementation.
     Each strategy should run in its **own isolated Goja VM instance**, analogous to a separate goroutine in Golang.
   - The application will read the **configured strategy path** from the configuration file to load JavaScript strategies.
   - Store all JavaScript strategy files in an external `strategies` directory (outside the main binary).

2. **Dynamic Loading & Hot Updates**
   - Implement a runtime loader that dynamically imports and manages JavaScript strategies from the external directory.
   - Allow full **CRUD** (Create, Read, Update, Delete) operations on strategies during runtime.
   - Support **hot updates** by adding or deleting strategy files without restarting the backend.
   - When a strategy is deleted or replaced, its corresponding Goja VM instance must be **gracefully unloaded and destroyed** to release resources safely.

3. **HTTP Control Endpoints**  internal/infra/server/http/server.go
   - Provide RESTful endpoints for:
     - Listing currently available strategies.
     - Performing CRUD operations on strategy files.
     - Triggering a **refresh** operation that reloads all strategies from the external directory.
   - The refresh endpoint should reinitialize all strategies from the configured path, replacing the in-memory strategy registry.

4. **Runtime Behavior** config/app.yaml internal/infra/config/app_config.go
   - Strategies are loaded **once** into application memory when the refresh endpoint is called.
   - File changes in the external directory **do not** take effect until the refresh API is explicitly invoked.
   - Loaded strategies remain active and isolated in memory until explicitly removed or replaced.
   - Each strategy executes within its **own goroutine**, with an independent Goja VM for isolation and concurrency control.

5. **Persistence and Metadata**
   - Strategy persistence is **purely in-memory**; no database or external storage is used.
   - Strategy **metadata** (e.g., name, version, description, parameters) must be declared **statically within the JavaScript file** itself.
   - The backend uses this embedded metadata to manage runtime information.

---

### Goal
Enable flexible, runtime management of trading strategies—supporting hot deployment, isolation, and in-memory performance—**without redeploying or recompiling** the backend binary.


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
