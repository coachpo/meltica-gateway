# Repository & MCP Guidelines

## Project Structure & Module Organization

- **Gateway entrypoint:** `cmd/gateway` wires pools, event bus, and REST control plane.
- **Core packages:** `internal/` split into `app/`, `domain/`, `infra/`, and `support/`.
- **Exported contracts:** `api/` for public-facing contracts.
- **Frontend & tooling:** `web/` (frontend) and `scripts/` (operator tooling).
- **Configuration:** defaults in `config/`.
- **Deployments:** assets in `deployments/`.
- **Architecture docs:** `docs/`.
- **Tests:**

  - Package-level suites colocated with sources.
  - Contract/architecture suites in `test/` and `tests/`.

- **MCP docs (this file):** checked into `docs/mcp/` as `repository-and-mcp-guidelines.md`. Link from `README.md`.

## Build, Test, and Development Commands

- **Run:** `make run` → `go run ./cmd/gateway/main.go` for fast feedback.
- **Build:** `make build` emits binaries into `bin/`.
- **Cross-build:** `make build-linux-arm64` cross-builds and copies YAML configs for packaging.
- **Quality gates:**

  - `make lint` runs `golangci-lint` (configured via `.golangci.yml`).
  - `make test` runs `go test ./... -race -count=1 -timeout=30s`.
  - `make coverage` enforces **≥ 70% TS-01** (view with `go tool cover -html=coverage.out`).

- **Performance:**

  - `make bench` benchmarks packages.
  - `make backtest STRATEGY=meanreversion` drives the offline runner.

- **MCP + CI:** If MCP tooling influenced code changes (e.g., automated refactors, doc retrieval), include an **MCP Tool Call Report** in the PR (see below). CI may parse it for auditability.

## Coding Style & Naming Conventions

- Format with `gofmt` (tabs, no spaces) and `goimports`.
- Respect lint bans:

  - Prefer `github.com/goccy/go-json` over `encoding/json`.
  - Prefer `github.com/coder/websocket` over Gorilla.

- Idiomatic identifiers:

  - **Exported:** PascalCase (types, funcs, consts).
  - **Internal:** lowerCamelCase.
  - **Packages:** short nouns (`eventbus`, `telemetry`).

- Avoid introducing identifiers: `legacy`, `deprecated`, `shim`, `feature_flag` — `forbidigo` will reject them.
- Document non-obvious flows with concise, adjacent comments.

## Testing Guidelines

- Prefer table-driven tests with descriptive suffixes (e.g., `TestDispatcher_Register/duplicateProvider`).
- Co-locate unit tests; share fixtures under `testdata/`.
- Contract suites in `tests/contract` expect deterministic fixtures; update snapshots before merging.
- Always run `make test` prior to a PR; gate merges with `make coverage` (70% minimum).
- Use focused benchmarks over ad‑hoc profiling to protect regression budgets.

## Commit & Pull Request Guidelines

- Follow conventional prefixes in `git log`: `feat:`, `fix:`, `docs:`, `refactor:`, etc., optional scope (e.g., `feat(dispatcher): …`).
- Summaries use imperative mood and stay under ~72 chars.
- **PR must include:**

  - Concise change description
  - Linked issues or strategy IDs
  - Checklist confirming `make lint` and `make test` passed
  - Screenshots/logs when UI or telemetry outputs change
  - **If MCP was used:** attach the **MCP Tool Call Report** section with parameters and outputs.

- Keep PRs focused; split large refactors into preparatory commits.

## Configuration & Telemetry Notes

- Create local configs via `cp config/app.example.yaml config/app.yaml` and adjust provider aliases, pool sizes, and telemetry endpoints before running.
- Control API defaults to `:8880`; document any port changes in PRs for dashboard alignment.
- When altering telemetry/metrics, update `docs/dashboards/` and `TELEMETRY_POINTS.md` alongside code so operators can redeploy collectors.

---

## MCP Calling Rules

### When to Use MCP

**Decision Tree**

```
Is the task achievable with local/offline tools?
├─ YES → Use local tools (grep, LSP, go test, etc.)
└─ NO  → Proceed to MCP evaluation
          ↓
          Does the task require external knowledge/automation?
          ├─ YES → Consult registry (this doc)
          │        ↓
          │        Select MCP based on:
          │        - Functional category match
          │        - Priority matrix (when overlap exists)
          │        - Scope minimization capability
          └─ NO  → Manual investigation or request user clarification
```

**Common Triggers**

- **Code ops:** multi-file symbol/reference tracking; batch refactors; non-interactive shell (build/test/lint) via MCP for validation.
- **Knowledge:** verify official docs, version diffs; practical patterns from wikis/blogs.
- **Planning:** break down >6‑step problems; track tasks and dependencies.
- **Automation & Testing:** browser E2E flows; visual regression; system file/process ops.
- **UI Dev:** design token extraction; component registry queries.

**Consultation Priority**

1. Local capabilities first (grep, LSP, `go vet`, etc.)
2. MCP registry (this document)
3. Apply priority matrix where overlaps exist
4. Verify prerequisites (network, credentials, permissions)

### How to Use MCP

**Core Principles**

1. **Prudent Single Selection**

   - Max **1 MCP per round** of interaction
   - Offline first; justify use explicitly

2. **Sequential Invocation**

   - Strictly serial; persist state between rounds

3. **Minimal Scope**

   - Precise parameters (paths, globs, tokens, limits)

4. **Traceability**

   - Pre-call intent + post-call **MCP Tool Call Report** (see below)

**Calling Workflow**

- **Preparation**: identify requirements → pick server via registry → apply priority matrix → verify prerequisites → define tight parameters.
- **Execution**: state intent → call with minimal scope → watch for errors → retry if needed (see Fallback).
- **Post-Processing**: validate outputs → append Tool Call Report → decide on next round → persist knowledge if needed (Memory server).

**Parameter Optimization**

| Aspect           | Guideline                     | Example                                     |
| ---------------- | ----------------------------- | ------------------------------------------- |
| Path Scoping     | Constrain to relevant subdirs | `path="internal/dispatcher"` not `path="."` |
| Pattern Matching | Include/exclude globs         | `paths_include_glob="**/*.go"`              |
| Token Limits     | Respect server constraints    | `tokens=3000` (≤ server max)                |
| Result Limits    | Cap to actionable sizes       | `limit=20`                                  |
| Time Bounds      | Use recency filters           | `recencyDays=14`                            |
| Domain Filters   | Whitelist official docs       | `domains=["go.dev","kubernetes.io"]`        |

### Fallback Strategies

**Error Handling**

| Error            | Action                        | Backoff | Retries |
| ---------------- | ----------------------------- | ------- | ------- |
| 429 Rate Limit   | Narrow scope/params           | 20s     | 1       |
| 5xx Server Error | Retry same params             | 2s      | 1       |
| Timeout          | Retry reduced scope           | 2s      | 1       |
| Empty Results    | Narrow query or request hints | -       | 0       |
| Network Failure  | Switch to offline fallback    | -       | 0       |

**Degradation Chain**

```
Primary MCP → Secondary per Priority Matrix → Local tools → Conservative answer (annotate uncertainty) → Ask for guidance
```

**Abort If** credentials missing/expired, network restricted, destructive ops without approval, ambiguity prevents meaningful results, or retries exhausted.

**Should**

- Consult registry before each call; document intent and parameters
- Use minimal scope and small result sets; respect server constraints
- Backoff on rate/5xx; record retries/fallbacks
- Persist key learnings via Memory server when applicable

**Shouldn’t**

- Call multiple MCPs in parallel
- Broad scans (`path="."`) without filters
- Speculative calls without concrete need
- Skip Tool Call Report

### MCP Tool Call Report

Add this block to PR descriptions or design docs whenever MCP was used:

```markdown
【MCP Call Report】
Service: <server-name>
Trigger: <specific reason>
Parameters: <key params summary>
Result: <hit count / main sources / output summary>
Status: <Success | Retry | Fallback>
```

**Examples**

- **Success**

  ```
  【MCP Call Report】
  Service: Serena
  Trigger: Locate all references to deprecated function `calculateTotal`
  Parameters: find_referencing_symbols(symbol="calculateTotal", path="internal/")
  Result: 23 references across 8 files (internal/billing, internal/reports)
  Status: Success
  ```

- **Fallback**

  ```
  【MCP Call Report】
  Service: Context7 → DeepWiki
  Trigger: Retrieve FastAPI middleware documentation
  Parameters: resolve-library-id("FastAPI") → get-library-docs(topic="middleware")
  Result: Context7 timeout → DeepWiki fallback → 3 wiki pages retrieved
  Status: Fallback (timeout → DeepWiki)
  ```

---

### Code Analysis & Refactoring

#### Serena

**Core Capabilities**

- LSP-backed semantic code analysis
- Multi-language symbol extraction & reference tracking
- Batch regex-based transformations with dry-run
- Shell command execution for build/test/lint validation

**Use Cases**

- Codebase onboarding & architectural analysis
- API migration and deprecation workflows
- Cross-repo refactoring; pre-commit validation

**Priority Matrix**

- vs **Desktop Commander**: repo-scoped → **Serena**; cross-app/filesystem → **Desktop Commander**
- vs **Sequential Thinking**: planning → **Sequential Thinking**; execution/validation → **Serena**

**API Surface**

| Function                                               | Description              | Usage                   |
| ------------------------------------------------------ | ------------------------ | ----------------------- |
| `get_symbols_overview(path?, depth?)`                  | Hierarchical symbol tree | Initial mapping         |
| `find_symbol(name, path?)`                             | Locate definitions       | Pre-refactor resolution |
| `find_referencing_symbols(symbol)`                     | All reference locations  | Impact analysis         |
| `replace_regex(glob, pattern, replacement, dry_run?)`  | Batch replace w/ preview | API/import migration    |
| `execute_shell_command(args[], workdir?, timeout_ms?)` | Run build/test/lint      | Post-change validation  |

---

### Documentation & Knowledge

#### Context7

**Core Capabilities**

- Version-specific docs retrieval from official sources
- API resolution & disambiguation
- Diff-based changelog analysis

**Use Cases**

- Verify API signatures & breaking changes
- Resolve deprecated/removed functionality
- Plan version upgrades

**Priority Matrix**

- vs **DeepWiki**: official API → **Context7**; practices/patterns → **DeepWiki**
- vs **DuckDuckGo**: canonical docs → **Context7**; recent community items → **DuckDuckGo**

**API Surface**

| Function                                | Description             | Usage           |
| --------------------------------------- | ----------------------- | --------------- |
| `resolve-library-id(libraryName)`       | Resolve to canonical ID | Disambiguation  |
| `get-library-docs(id, tokens?, topic?)` | Fetch docs by topic     | Targeted lookup |

#### DeepWiki

**Core Capabilities**

- GitHub repo wiki aggregation & indexing
- Project best practices & contribution guides
- Semantic wiki search

**Use Cases**

- Engineering patterns & ADRs
- Release & deployment procedures
- Onboarding and conventions

**Priority Matrix**

- vs **Context7**: lack official docs or need examples → **DeepWiki**; formal API → **Context7**
- vs **DuckDuckGo**: project-specific knowledge → **DeepWiki**; external discussions → **DuckDuckGo**

**API Surface**

| Function                       | Description           | Usage             |
| ------------------------------ | --------------------- | ----------------- |
| `read_wiki_structure(repo)`    | Enumerate wiki ToC    | Navigation        |
| `read_wiki_contents(repo)`     | Retrieve full content | Offline analysis  |
| `ask_question(repo, question)` | Semantic Q&A          | Direct extraction |

#### DuckDuckGo Search

**Core Capabilities**

- Web search with recency/domain filters
- Content extraction & ranking
- Privacy-focused search

**Use Cases**

- Recent issues & bug discussions
- Release announcements
- Tutorials & blog posts

**Priority Matrix**

- Fallback when official/community docs insufficient

**API Surface**

| Function                                | Description         | Usage                |
| --------------------------------------- | ------------------- | -------------------- |
| `search(query, recencyDays?, domains?)` | Search with filters | Time-bounded queries |

---

### Task Management & Planning

#### Sequential Thinking

**Core Capabilities**

- Multi-step problem decomposition w/ revisions
- Hypothesis generation & verification
- Dynamic planning with branching/backtracking

**Use Cases**

- Architecture & refactoring plans
- Perf optimization strategies
- Cross-system integration design

**Priority Matrix**

- vs **MCP Shrimp**: planning → **Sequential Thinking**; tracking/verification → **MCP Shrimp**

**API Surface**

| Function                  | Description                        | Usage            |
| ------------------------- | ---------------------------------- | ---------------- |
| `sequentialthinking(...)` | Structured thinking with revisions | Complex planning |

#### MCP Shrimp Task Manager

**Core Capabilities**

- NL → structured tasks; dependency tracking
- Verification with scoring & quality gates
- Execution history & knowledge base

**Use Cases**

- Feature workflow management
- Sprint planning & breakdown
- QA and acceptance testing

**Priority Matrix**

- vs **Sequential Thinking**: plan first → **Sequential Thinking**; execute/track → **MCP Shrimp**
- vs **Memory**: task-specific knowledge → **MCP Shrimp**; long-term patterns → **Memory**

**API Surface**

| Function                                       | Description          | Usage          |
| ---------------------------------------------- | -------------------- | -------------- |
| `init_project_rules()`                         | Initialize standards | Project setup  |
| `plan_task(description, requirements?)`        | Actionable plan      | Task creation  |
| `split_tasks(tasksRaw, updateMode, ...)`       | Decompose tasks      | Breakdown      |
| `list_tasks(status)`                           | Query by status      | Tracking       |
| `get_task_detail(id)`                          | Retrieve details     | Inspection     |
| `analyze_task(...)`                            | Complexity analysis  | Risk           |
| `execute_task(id)`                             | Execution guidance   | Implementation |
| `update_task(id, ...)`                         | Modify task          | Iteration      |
| `verify_task(id, score, summary)`              | Quality gate         | Acceptance     |
| `delete_task(id)` / `clear_all_tasks(confirm)` | Lifecycle mgmt       | Cleanup        |

#### Memory

**Core Capabilities**

- Persistent knowledge graph (entities, relations, observations)
- Semantic search & traversal; causal chains
- Long-term context retention

**Use Cases**

- ADRs and decision rationale
- Incident post-mortems and RCA
- Team knowledge base

**Priority Matrix**

- Post-execution persistence → **Memory**

**API Surface**

| Function                      | Description     | Usage              |
| ----------------------------- | --------------- | ------------------ |
| `create_entities(entities[])` | Define nodes    | Modeling           |
| `add_observations(...)`       | Record facts    | Logging            |
| `create_relations(...)`       | Link nodes      | Graph construction |
| `search_nodes(query)`         | Semantic search | Retrieval          |
| `open_nodes(names[])`         | Node details    | Inspection         |
| `read_graph()`                | Export graph    | Analysis           |
| `delete_*`                    | Maintenance     | Curation           |

---

### Browser Automation & Testing

#### Playwright MCP

**Core Capabilities**

- Cross-browser automation (Chromium/Firefox/WebKit)
- A11y-tree based element interaction; screenshots & network
- Multi-tab sessions; optional vision/PDF/testing/tracing tools

**Use Cases**

- E2E test generation & maintenance
- Form automation and workflow testing
- Visual regression & network debugging

**Priority Matrix**

- vs **Chrome DevTools**: portable testing → **Playwright**; deep profiling → **Chrome DevTools**
- vs **Desktop Commander**: web automation → **Playwright**; native/filesystem → **Desktop Commander**

**API Surface (highlights)**

- Core automation: navigate, click, type, fill forms, select, hover, drag, upload, dialogs, keys, evaluate, wait, resize, screenshot, console, network, close
- Tabs: list/create/close/select
- Optional: install, coordinate/vision, PDF, testing assertions, tracing

#### Chrome DevTools MCP

**Core Capabilities**

- Automation with a11y snapshots; performance profiling (CWV)
- Network monitoring; console logs; device emulation

**Use Cases**

- Performance bottleneck analysis with traces
- Automated testing & debugging
- Responsive verification; production diagnostics

**Priority Matrix**

- vs **Playwright**: profiling & DevTools integration → **Chrome DevTools**; cross-browser validation → **Playwright**

**API Surface (highlights)**

- Input & navigation automation; emulation; performance tracing; network inspection; debugging/snapshots

---

### System Operations

#### Desktop Commander

**Core Capabilities**

- Filesystem CRUD & search; process management; cross-platform shell
- Audit logging & usage analytics

**Use Cases**

- Batch file processing; log collection; environment setup
- System diagnostics and troubleshooting

**Priority Matrix**

- vs **Serena**: repo-agnostic ops → **Desktop Commander**; code-aware ops → **Serena**
- vs **Playwright**: native & filesystem → **Desktop Commander**; web → **Playwright**

**API Surface (highlights)**

- FS operations (create/write/edit/move/read/list/info), process lifecycle, searches, config, usage stats

---

### Design & UI Development

#### Figma

**Core Capabilities**

- File structure parsing & node traversal
- Design token extraction; asset export (SVG/PNG)
- Component & annotation retrieval

**Use Cases**

- Design system implementation & validation
- Asset pipeline automation; design-to-code handoff

**Priority Matrix**

- vs **shadcn**: tokens/assets → **Figma**; component implementation → **shadcn**

**API Surface**

- `get_figma_data(...)`, `download_figma_images(...)`

#### shadcn

**Core Capabilities**

- shadcn/ui registry integration; source & docs retrieval
- Usage examples; install command generation; audit checklist

**Use Cases**

- Component selection, scaffolding, design consistency

**Priority Matrix**

- vs **Figma**: design extraction → **Figma** first; component implementation → **shadcn**

**API Surface**

- Registry listing/search/view; examples; install commands; audit checklist

---

## Server Entry Template & Documentation Standards

Use this template when documenting **new** MCP servers:

```markdown
#### Server Name

**Repository:** `https://github.com/org/repo`

**Core Capabilities:**

- Key capability 1
- Key capability 2
- Key capability 3

**Use Cases:**

- Specific scenario 1
- Specific scenario 2
- Specific scenario 3

**Priority Matrix:**

- vs **Server X**: When to use this server vs Server X
- vs **Server Y**: When to use this server vs Server Y

**API Surface:**

| Function                   | Description  | Usage Pattern  |
| -------------------------- | ------------ | -------------- |
| `function_name(params)`    | What it does | When to use it |
| `another_function(params)` | What it does | When to use it |
```

**Documentation Standards**

- **Repository**: Link to official or most authoritative implementation.
- **Core Capabilities**: Technical features, not business outcomes.
- **Use Cases**: Concrete scenarios with verifiable outcomes.
- **Priority Matrix**: Decision criteria when multiple servers overlap.
- **API Surface**: Table format for scannability; include parameter signatures.

---

### Governance & Compliance Notes

- **Security:** Never submit credentials, tokens, or PII via MCP calls. Redact logs in Tool Call Reports.
- **Destructive Ops:** Require explicit maintainer approval before filesystem/process-altering calls on production systems.
- **Auditability:** Keep MCP reports in PRs or `/docs/mcp/reports/` for notable migrations/refactors.
- **CI Hooks:** Future CI can parse `【MCP Call Report】` blocks to enforce traceability.
