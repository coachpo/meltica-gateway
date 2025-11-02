# Universal Engineering Constitution

## 1. Foundational Alignment

**FA-01: Problem Statement Before Solution (MUST)**  
Document a one-sentence problem statement describing the audience and pain before implementation begins; store it in the shared plan or specification.

Rationale: Shared context prevents teams from solving the wrong problem and anchors scope decisions.

**FA-02: Measurable Outcomes Declared (MUST)**  
Define objective success metrics or acceptance conditions prior to writing code; releases are blocked until these targets are recorded.

Rationale: Clear outcomes enable verification, prioritisation, and focused iteration.

**FA-03: Stakeholder Alignment Recorded (SHOULD)**  
List the accountable owner, decision makers, and dependent teams with confirmation that they agree to the problem and outcomes.

Rationale: Transparent accountability reduces costly rework caused by late alignment.

## 2. Architectural Integrity

**AI-01: Explicit Boundary Map (MUST)**  
Outline the system components/modules and allowed interactions before altering architecture; update the map whenever boundaries change.

Rationale: Intentional boundaries prevent accidental coupling and simplify reasoning about change impact.

**AI-02: Versioned External Contracts (MUST)**  
Expose a version identifier and migration plan for every externally consumed interface (APIs, events, files, schemas); document any incompatible change.

Rationale: Versioning allows safe evolution and protects downstream consumers from surprise regressions.

**AI-03: Intentional Dependency Direction (SHOULD)**  
Keep dependencies single-directional and interface-driven; exceptions require documented justification and a follow-up cleanup plan.

Rationale: Controlled dependency graphs keep the system understandable and maintainable.

## 3. Delivery Workflow

**DW-01: Single Decision Owner (MUST)**  
Name one accountable owner for each initiative who is empowered to make scope and trade-off decisions.

Rationale: Clear ownership accelerates decision making and avoids conflicting directives.

**DW-02: Canonical Artifacts (MUST)**  
Plans, specifications, research notes, and tasks remain the source of truth; untracked side channels must not introduce requirements.

Rationale: A single canonical record ensures traceability and keeps contributors aligned.

**DW-03: Change Log (MUST)**  
Record material changes to scope, constraints, or timelines with timestamp, rationale, and approver within the canonical artifact.

Rationale: Change history enables audits, post-mortems, and informed future decisions.

## 4. Code Quality Practices

**CQ-01: Intent-Revealing Interfaces (MUST)**  
Name and document public interfaces, modules, and functions to describe behaviour, invariants, and responsibilities.

Rationale: Expressive code shortens onboarding time and lowers review overhead.

**CQ-02: Dependency Discipline (MUST)**  
Code may interact only through declared interfaces; cross-boundary shortcuts require a documented exemption.

Rationale: Enforcing dependency rules preserves modularity and isolates risk.

**CQ-03: No Silent Failure (MUST)**  
Errors, exceptional conditions, and degraded modes must surface through explicit results, error types, or alerts—never silently ignored.

Rationale: Transparent failure handling prevents hidden defects and accelerates incident resolution.

**CQ-04: Remove Dead Paths Quickly (SHOULD)**  
Eliminate unused code, feature flags, and experiments within the iteration they become obsolete.

Rationale: Removing dead code reduces cognitive load and minimises attack surface.

## 5. Testing & Verification

**TV-01: Automated Coverage for Critical Journeys (MUST)**  
Primary user journeys and core system workflows require automated tests before launch; choose unit, integration, or contract tests as appropriate.

Rationale: Automated coverage protects high-impact behaviour and enables confident change.

**TV-02: Deterministic, Isolated Tests (MUST)**  
Tests run deterministically, control time, avoid shared mutable state, and enforce timeouts to prevent hangs.

Rationale: Deterministic tests build trust in automation and keep pipelines reliable.

**TV-03: Continuous Verification Gates (MUST)**  
Static analysis, linting, and automated tests execute on every merge or release candidate; failures block deployment until resolved.

Rationale: Continuous gates catch regressions early and uphold quality standards.

## 6. Operational Excellence

**OP-01: Observable Critical Flows (MUST)**  
Instrument logs, metrics, or traces for critical paths and define alert ownership.

Rationale: Observability data is essential for diagnosing issues quickly.

**OP-02: Fast Recovery Plan (MUST)**  
Document rollback, restart, and data recovery procedures before shipping; verify they are executable.

Rationale: Prepared recovery plans reduce downtime and limit impact.

**OP-03: Progressive Delivery Safeguards (SHOULD)**  
Use staged rollouts, feature toggles with retirement plans, or other blast-radius controls for impactful changes.

Rationale: Progressive delivery limits risk while validating changes in production.

## 7. Security & Privacy

**SP-01: Least Privilege Access (MUST)**  
Grant only the access required to perform duties and review permissions regularly.

Rationale: Minimising privilege reduces the attack surface and insider risk.

**SP-02: Protect Sensitive Data (MUST)**  
Classify sensitive data, enforce encryption or masking in transit and at rest, and prevent secrets from appearing in logs.

Rationale: Strong data protections prevent breaches and regulatory violations.

**SP-03: Security Review Before Launch (MUST)**  
Conduct a threat assessment or checklist review for new capabilities; capture findings and mitigations before release.

Rationale: Proactive reviews surface vulnerabilities before exposure.

## 8. Governance Framework

**GOV-01: Amendment Authority (MUST)**  
A designated maintainer records amendments, including rationale and impacted principles; external contributions require explicit approval.

Rationale: Controlled stewardship keeps the constitution coherent and authoritative.

**GOV-02: Semantic Versioning (MUST)**  
Apply semantic versioning: MAJOR for structural or breaking governance changes, MINOR for new principles or substantial expansions, PATCH for clarifications.

Rationale: Predictable versioning communicates expected adoption effort.

**GOV-03: Compliance Checkpoints (MUST)**  
Each initiative confirms compliance with all MUST principles during planning and before release, logging evidence in the canonical artifact.

Rationale: Formal checkpoints keep the constitution actionable instead of symbolic.

**GOV-04: Transparent Decisions (SHOULD)**  
Store major decisions, exceptions, and trade-offs in an accessible decision log linked from the plan or specification.

Rationale: Transparency accelerates onboarding and supports retrospectives.

### 8.1 Amendment Procedure

1. Draft the proposed change, listing affected principles and the motivation.
2. Obtain maintainer approval and record the decision in the change log.
3. Update the constitution content, version number, dates, and impacted templates.
4. Communicate required follow-up actions and track them to completion.

### 8.2 Versioning Policy

- MAJOR: Replaces or removes existing principle families or governance structures.
- MINOR: Adds new principles, sections, or materially expands existing guidance.
- PATCH: Clarifies wording without changing intent.

### 8.3 Compliance Review Expectations

- **Planning Gate**: Confirm FA, AI, DW, CQ, TV, OP, SP, and GOV MUST items are addressed before design work proceeds.
- **Pre-Release Gate**: Re-validate compliance and document test results, observability readiness, recovery steps, and security sign-off.
- **Post-Incident Gate**: Assess adherence during incident reviews and feed learnings back into artifacts.

### Appendix: Severity Levels

- **MUST**: Blocking requirement; do not knowingly violate.
- **SHOULD**: Strong recommendation; justify when diverging.
- **INFO**: Advisory; best practice guidance.

**Version**: 6.0.0 | **Ratified**: 2025-10-12 | **Last Amended**: 2025-10-15

---

## Version History

### v6.0.0 (2025-10-15)

MAJOR: Replaced Meltica-specific architecture, tooling, and component mandates with the technology-agnostic Universal Engineering Constitution. Introduced FA, AI, DW, CQ, TV, OP, SP, and GOV principle families plus explicit amendment, versioning, and compliance procedures.

### v5.1.0 (2025-10-14)

MINOR: Enhanced PERF-06 with fan-out duplicate strategy using sync.Pool for per-subscriber copies and parallel delivery. Enhanced PERF-07 with Recycler as single return gateway, debug poisoning to catch use-after-put, and double-put guards. Added PERF-08 (Consumer Purity Rules): pure lambdas, routing_version-based market-data ignoring, critical kinds (ExecReport, ControlAck, ControlResult) always delivered. Added PERF-09 (Concurrency Library Standard): MUST use github.com/sourcegraph/conc, FORBID async/pool. Updated GOV-02b to include async/pool in banned imports. Updated GOV-04 to explicitly mandate async/pool eradication. Added GOV-06 (Developer Workflow Guidance): use context7 prompt for Cursor/agents to get current library docs. No backward compatibility per CQ-08/GOV-04.

### v5.0.0 (2025-10-13)

MAJOR: Added mandatory runtime performance & memory requirements. Introduced PERF-04 (MUST use goccy/go-json, FORBID encoding/json), PERF-05 (MUST use coder/websocket, FORBID gorilla/websocket), PERF-06 (object pooling with sync.Pool for canonical events and hot-path structs), PERF-07 (struct bus ownership rules: Dispatcher fan-out clones, pool lifecycle). Updated GOV-02b to include CI guards for banned imports and coverage enforcement. Removed duplicate principles (old PERF-06, UX-06). No backward compatibility layers per CQ-08/GOV-04.

### v4.0.0 (2025-10-12)

Redefined principles for a loss-tolerant, non-HFT monolith: immutable boundaries; canonical, versioned events; per-stream ordering; explicit backpressure; windowed merge rules; idempotent orders; provider-side book assembly; ops-only telemetry; simple restarts; and stricter testing gates (coverage/timeouts).

### v3.0.0 (2025-10-12)

Introduced Meltica Event Pipeline principles (EP-01 through EP-08) to mandate canonical-first ingestion, Dispatcher authority, control plane isolation, end-to-end observability, and refactor directives (Router → Dispatcher, Coordinator removal, Filter Adapter replacement).

### v2.1.0 (2025-10-12)

Added CQ-10: Static Code Inspection principle requiring `golangci-lint` for all code changes. Updated GOV-02b (Automated Enforcement Inventory) and GOV-04 (Quality Gates) to reflect the new linting requirement. Makefile already provides `make lint` target; developers must resolve linter issues before committing.

### v2.0.0 (2025-10-12)

Lean solo-dev rewrite. Removed bureaucratic governance (RFCs, formal reviews), simplified testing to essentials, aligned with current CI (build + `go test -race`), and codified "MUST ALWAYS IGNORE BACKWARD COMPATIBILITY".
