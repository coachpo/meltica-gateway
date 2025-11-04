# Strategy Module Validation & Diagnostics Plan

## Summary

Strengthen server-side validation for uploaded JavaScript strategy modules so the gateway rejects invalid code with structured, user-friendly diagnostics. This plan targets the bugs and suggestions described in `meltica-ui-feedback.md` (syntax error messaging, metadata validation, and author guidance) while keeping the offline authoring workflow unchanged.

## Goals

- Fail fast when compilation or metadata extraction encounters errors, returning actionable diagnostics (line/column, code frame, suggestion).
- Guarantee required metadata (`metadata.events`, `displayName`, `config`) so runtime consumers and viewers cannot crash on missing fields.
- Provide reusable validation utilities to support future contract tests and tooling.

## Current State

- `internal/app/lambda/js/loader.go` compiles uploaded modules using `goja.Compile`, but wraps failures in generic `fmt.Errorf` strings (`strategy loader: compile %q: %w`). The UI receives a raw message without positional data (`meltica-ui-feedback.md:7`).
- The loader only checks for `metadata` existence and `name`, allowing modules with empty `events` arrays to persist (`meltica-ui-feedback.md:8`).
- API responses from module creation/update do not expose machine-readable error structures, making client UX improvements difficult.

## Key Enhancements

### 1. Structured Compilation Diagnostics

- Capture `*goja.Exception` when `goja.Compile` or `rt.RunProgram` fails; extract `FileName()`, `LineNumber()`, `Column()` and normalize path to the uploaded filename.
- Introduce `internal/app/lambda/js/diagnostics.go` with helper types:
  ```go
  type Diagnostic struct {
      Stage    string `json:"stage"` // compile | execute | validation
      Message  string `json:"message"`
      Line     int    `json:"line,omitempty"`
      Column   int    `json:"column,omitempty"`
      Hint     string `json:"hint,omitempty"`
  }
  ```
- Ensure `compileModule` returns `*DiagnosticError` that aggregates diagnostics so API layers can serialize them cleanly.

### 2. Metadata Validation

- Extend `extractMetadata` to assert:
  - `DisplayName` present and <= 80 chars.
  - `Events` non-empty and contains only known `schema.EventType` constants.
  - `Config` fields have `Name`, `Type`; inject `dry_run` using existing helper.
- Return diagnostics with `Stage: "validation"` highlighting missing or invalid metadata entries.
- Provide reusable validation function under `internal/app/lambda/strategies` for sharing with tests and potential CLI tooling.

### 3. API Contract Updates

- Modify the handler that persists modules (likely `internal/app/...` orchestrator invoked from `cmd/gateway`) to map `DiagnosticError` to HTTP 422 with payload:
  ```json
  {
    "error": "strategy_validation_failed",
    "message": "Compilation failed",
    "diagnostics": [
      { "stage": "compile", "message": "...", "line": 42, "column": 3 }
    ]
  }
  ```
- Document the new schema in `api/` if public, and update any client SDK types (`web/client/src/lib/types.ts`).

### 4. Telemetry & Logging

- Log diagnostics at `INFO` with correlation IDs so support can trace failures without exposing raw stack traces to clients.
- Emit a counter metric (e.g., `strategy.upload.validation_failure_total` labeled by `stage`) for monitoring.

## Testing Strategy

- Unit tests in `internal/app/lambda/js/loader_test.go` covering:
  - Syntax error yields `line` and `column`.
  - Missing `metadata.events` surfaces descriptive validation diagnostic.
  - Invalid `schema.EventType` enumerations rejected.
- Contract/handler tests simulating API requests to confirm 422 payload structure.
- Integration regression in `tests/contract/ws-routing` (if applicable) ensuring valid modules still register successfully.

## Rollout Plan

1. Implement diagnostics helpers and loader changes behind feature flag (env var `STRATEGY_VALIDATION_STRICT=true`) for canary.
2. Update gateway handlers to surface 422 responses when flag enabled.
3. Deploy to staging, upload known-bad modules, confirm UI receives structured payload.
4. Remove flag after validation; communicate contract change to frontend team.

## Risks & Mitigations

- **Existing consumers expect 500 errors:** adjust API docs, provide compatibility window with opt-in flag.
- **Exposing internal file paths:** normalize filenames (`uploaded.js`) and strip absolute paths before returning diagnostics.
- **Performance impact on upload path:** caching not required but ensure validation logic avoids heavy reflection; profile uploads with large modules.

## Dependencies

- Coordination with frontend to consume diagnostics (see companion plan in `docs/plans/frontend/3-strategy-module-editor-plan.md`).
- Possible updates to architecture tests if new packages introduced.

## Deliverables

- Diagnostic utility module and updated loader.
- New/updated tests demonstrating validation behavior.
- API documentation addendum and release notes describing new error contract.
