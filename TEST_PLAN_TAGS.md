# Strategy Module Tag Management UI – Test Plan

## 1. Overview
- **Feature**: Docker-style tag management on `/strategies/modules`
- **Objective**: Ensure operators can view, create, reassign, and delete tags reliably, with UI reflecting backend state immediately.
- **Scope**: Browser UI + gateway API integration.

## 2. Environment & Prerequisites
1. **Services**
   - Frontend dev server (`pnpm dev`) at `http://localhost:3000`.
   - Backend gateway (`make run` or equivalent) at `http://localhost:8880`.
2. **Seed Data**
   - Strategies such as `logging`, `grid`, `noop` with multiple revisions and tags (`latest`, `v1.0.1`, etc.).
3. **Tools**
   - Chromium-based browser with DevTools.
   - Optional Playwright MCP for automation/regression.
4. **Reset Guidance**
   - After destructive operations, reapply tags via API or re-seed from git to keep baseline consistent for next runs.

## 3. Test Cases

### 3.1 Baseline Visibility & Integrity
1. Load `/strategies/modules`.
2. Validate tag chips in main table match API response (`tags[]`).
3. Open metadata drawers for at least two modules; ensure “Tags” table lists all aliases with correct ordering and `default` indicator.
4. Copy tag hashes and compare with `Download registry` JSON to confirm backend parity.

### 3.2 Create New Tag (Alias)
1. In `logging` drawer → `Revision history`, select a revision lacking custom tags.
2. Click `Tag`, input `qa-smoke`, keep “Refresh runtime” on, submit.
3. Expect success toast; verify `qa-smoke` appears instantly in both drawer and table.
4. Repeat with refresh toggled **off** to confirm no refresh toast but UI still updates.

### 3.3 Reassign Existing Tag (Move)
1. Use Tag dialog to move `latest` to another revision.
2. Confirm response shows `previousHash`, drawer marks new default, table reflects change.
3. Ensure running instances stay pinned until manual “Refresh catalog”.
4. Move a non-latest alias (e.g., `qa-smoke`) to a third revision; verify alias leaves old revision entry.

### 3.4 Delete Tag
1. In Tags table, remove `qa-smoke`; confirm DELETE succeeds, tag disappears without manual refresh.
2. Attempt to remove `latest`—button must be disabled (no network call).
3. Use API parameter `allowOrphan=true` (via query string) to confirm backend honors optional flag, UI still refetches.

### 3.5 Error Handling
1. Submit Tag dialog with blank/invalid name → inline error.
2. Toggle browser offline before submitting to simulate network failure; ensure spinner stops, toast shows error.
3. Remove dialog: cancel action and verify no change/no fetch.

### 3.6 Cross-Module Regression
1. Filter to another strategy (e.g., `noop`), add/delete a tag to ensure state isolation.
2. Navigate to another page (`/strategies`) and back; verify tags persist across navigation.

### 3.7 Concurrency & Cache
1. Open two tabs. In Tab A, move `latest`; in Tab B, click “Refresh catalog” to ensure alias updates.
2. After delete in Tab A, download registry to confirm alias removed; Tab B should reflect removal after refresh.

### 3.8 Accessibility & Keyboard
1. Use keyboard (Tab/Shift+Tab) to navigate Tag dialog; `Enter` submits, `Esc` cancels.
2. Remove dialog: `Enter` confirms, `Esc` cancels; focus returns to trigger button.

### 3.9 Negative API Guardrails
1. Try deleting a non-existent tag via edited network request → expect backend 404 surfaced in UI.
2. Attempt to reassign tag with invalid hash; ensure backend error is shown and UI remains stable for retry.

## 4. Success Criteria
- All tag operations (view/create/move/delete) reflected immediately without full page reload.
- Protected tags (`latest`) can be reassigned but not deleted.
- Error states present clear inline/toast messages and allow retry.
- UI state remains consistent across navigation and multi-tab workflows.

## 5. Reporting
- Capture failures with exact steps, screenshots, console and network logs.
- Share backend responses (status + payload) for API errors.
- Restore baseline tags (e.g., `latest` pointing to canonical hash) after testing to keep environment clean for future runs.

