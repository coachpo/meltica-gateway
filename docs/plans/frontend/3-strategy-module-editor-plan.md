# Strategy Module Editor UX Plan

## Summary

Improve the strategy module upload/edit experience for developers who author JavaScript offline and either paste or upload the file. The updated UI must present clear guidance, highlight errors returned by the backend, and maintain existing workflow affordances (file picker, manual paste, dialogs).

## Goals

- Surface compilation and metadata validation issues with precise, user-friendly messaging.
- Preserve lightweight offline workflow while offering optional scaffolding guidance.
- Maintain accessibility and performance within the existing Next.js (React 19) application.

## Personas & Constraints

- **Strategy Engineer:** Authors JS modules in external IDE, expects quick validation feedback after upload.
- **Operations Reviewer:** Needs to inspect source/read-only previews to verify deployments.
- Environment uses Radix dialogs and Shadcn components; editor must work in modal context and respect tailwind theming.

## Feature Enhancements

### 1. Error Presentation

- Update save handler in `web/client/src/app/strategies/modules/page.tsx` to parse HTTP 422 payload produced by backend (`error`, `message`, `diagnostics` array).
- For diagnostics with `line`/`column`, annotate the editor (Ace or existing textarea) with inline markers; fall back to top-of-form alert summarizing issues.
- Translate backend `stage` to human copy (`Compile error`, `Metadata validation`, `Runtime init`) and provide call-to-action text.
- Keep toast notifications for success paths; use inline alert for blocking issues to avoid dismissal confusion.

### 2. Author Guidance

- Add optional “Insert template” button near the editor that drops a minimal module skeleton including `metadata.events`, `displayName`, and configuration example.
- Provide inline hint text describing required metadata fields with links to docs (`docs/strategies`) and recommended version/tag rules.
- When file upload succeeds, auto-populate editor and reset annotations; show filename and size metadata next to the field for confirmation.

### 3. Editor Experience

- Evaluate integrating Ace as a lightweight improvement: syntax highlighting, bracket matching, read-only mode reuse for source viewer dialog.
- Wrap editor in a client-only component (`'use client'`, dynamic import) to avoid SSR issues; ensure `spellCheck={false}` and `aria-label` set.
- Maintain keyboard accessibility: focus trap within Radix dialog, `Ctrl/Cmd+Enter` to submit, `Escape` to close, screen reader announcements on validation errors.
- Provide responsive height adjustments (min 320px, auto grow to 60vh on desktop) while keeping virtualization performant.

### 4. State & Data Handling

- Track diagnostics in component state so they can be cleared when the user edits or reloads a file.
- Extend `web/client/src/lib/types.ts` with `StrategyErrorResponse` to describe new payload; update API client to throw custom `StrategyValidationError` containing diagnostics.
- Guard against duplicate submissions by disabling buttons while request is pending; ensure spinner messaging remains accessible.

## Testing & QA

- Add unit tests for `apiClient` error handling to confirm diagnostics parsing.
- Write React Testing Library tests for the module form covering:
  - Rendering template insertion.
  - Displaying compile error annotation.
  - Clearing errors when input changes.
- Manual QA checklist:
  1. Paste module with syntax error → inline marker + friendly message.
  2. Upload module missing `metadata.events` → validation message referencing missing field.
  3. Successful save leaves dialog, triggers toast, refreshes revision list.
  4. Verify keyboard navigation within dialog, including editor focus transitions.

## Metrics & Telemetry

- Emit frontend telemetry event (`strategy_module.validation_failure`) with stage + presence of diagnostics to measure effectiveness.
- Consider logging anonymized counts of template usage to gauge adoption.

## Rollout Plan

1. Implement diagnostics parsing and UX updates behind feature flag (`NEXT_PUBLIC_STRATEGY_VALIDATION_UI=true`).
2. Release to staging, coordinate with backend canary to ensure payload compatibility.
3. Conduct usability review with strategy engineers; gather feedback on template wording.
4. Enable feature flag in production once backend changes are live; monitor support channels for regressions.

## Dependencies

- Backend validation plan (`docs/plans/backend/3-strategy-module-validation-plan.md`) must land first to supply diagnostics payload.
- Potential Ace integration requires bundler impact review; ensure new dependency aligns with repo policies.

## Open Questions

- Do we need diff view between revisions within the same dialog? If yes, scope additional iteration.
- Should we provide localized error copy for non-English users? Currently out of scope but note for future internationalization passes.
