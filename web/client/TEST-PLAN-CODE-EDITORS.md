# CodeEditor & CodeViewer Test Plan

## 1. Approach

| Layer | Tooling | Focus |
| --- | --- | --- |
| Unit / Component | Vitest or Jest + React Testing Library (with `react-ace` mocked) | Prop plumbing, loader caching, read-only enforcement, command injection, placeholder states |
| Integration (Next.js consumers) | React Testing Library & jest mocks | StrategyModuleEditor, context backup page, dialog sizing, state wiring |
| End-to-End | Playwright (against `pnpm dev`) | Real Ace behavior: sizing, scrolling, shortcuts, search/replace, folding, clipboard, accessibility, security |
| Manual / Performance | Large fixtures + manual observation | Large files, very long lines, scroll smoothness, memory usage |

Key practices:
- Mock `react-ace` in unit tests to keep DOM light, but assert loader invocations.
- For Playwright, script workflows on `/strategies/modules` and `/context/backup` to exercise both editor and viewer.
- Capture accessibility snapshots (focus order, `aria-label`s) and verify no console errors (esp. asset loading).
- Responsive audits via viewport changes (375px mobile, 768px tablet, 1280px desktop).

## 2. Test Cases

### 2.1 CodeEditor Rendering
1. `width`/`height` props show in wrapper & Ace props.
2. `minLines`/`maxLines` adjust session; verify via Ace API mock.
3. `fontSize` updates Ace `setOptions.fontSize`.

### 2.2 CodeViewer Read-only
1. `readOnly` ensures no `onChange` calls, typing blocked but selection allowed.
2. Copy preserves original EOLs.
3. `showGutter` toggles line numbers, gutter width adapts to large counts.

### 2.3 Loader Behavior
1. Base Ace script loads once; per-mode/theme/extras cached (spy on `import`).
2. Unknown mode/theme gracefully skipped → plain text mode, no crash.

### 2.4 Wrap & Scroll
1. `wrapEnabled=true` removes horizontal scrollbar for long lines; `false` keeps horizontal scroll.
2. Vertical scrollbar appears only when content exceeds viewport; wheel scroll smooth.
3. Fixed-height viewers/editors expose vertical and horizontal scrollbars when content exceeds container bounds.

### 2.5 Syntax & Themes
1. `mode="javascript"` tokenizes JS sample; switching to JSON updates tokens.
2. Theme swap (tomorrow → github) updates CSS class without flicker.
3. Editor and viewer adopt the active application theme when toggling between light and dark modes.

### 2.6 Gutter & Line Counts
1. With `showGutter=true` on 1000+ lines, gutter width expands and numbers stay visible.
2. `showGutter=false` removes gutter area without leftover padding.

### 2.7 Word Wrap Toggle
1. Toggling `wrapEnabled` retains cursor/selection & scroll position.

### 2.8 Whitespace & Tabs
1. `tabSize` = 2 vs 4 affects indentation width.
2. `setOptions.displayIndentGuides=true` renders guides.
3. Typing Tab inserts spaces matching configured size.

### 2.9 EOL Handling
1. Load CRLF content, copy → clipboard retains CRLF.
2. Mixed CR/LF lines display consistently and no crash.

### 2.10 Accessibility
1. Editor wrapper focusable (Tab) with visible focus ring.
2. `aria-label` announced via screen reader baseline.
3. Keyboard navigation (Ctrl+Home/End, Shift+Arrow) works without mouse.

### 2.11 Security
1. Pasting `<script>` renders literal text; DOM untouched.
2. Copying HTML shows tags verbatim; no HTML execution.

### 2.12 Editor Editing Features
1. Typing, selection, caret movement via keys/mouse.
2. Undo/redo multi-step groups (Ctrl/Cmd+Z / Ctrl/Cmd+Y).
3. Folding markers appear for supported modes; fold/unfold updates gutter indicators.
4. Search/replace (Ctrl+F, Ctrl+Alt+F) works with wrap-around & replace-all counts.
5. Bracket matching highlight & auto-pair insertion when behaviours enabled.
6. Clipboard & drag-drop preserve content and EOLs.
7. Column selection (Alt+Drag) works when enabled.

### 2.13 API Events
1. `onLoad` receives Ace editor instance; calling `editor.setValue` updates view.
2. `onChange`, `onSelectionChange`, `onCursorChange` fire with expected payloads.
3. `annotations` prop paints gutter markers & highlighted lines.

### 2.14 Viewer/Editor Integration
1. StrategyModuleEditor `useEnhancedEditor` toggles diagnostics, gutter, submit shortcut.
2. Context backup page shows CodeViewer read-only snapshot, CodeEditor import with validation messaging.
3. View source dialog keeps viewer height within 60vh wrapper; internal scroll only.

### 2.15 Responsive Checks
1. Mobile viewport (375px): editors fit width, min height respected, no horizontal scroll.
2. Tablet/desktop: `width="100%"` spans container; dialogs center and remain scrollable.

### 2.16 Performance / Large Input
1. Load 10k-line file: typing latency <200 ms, memory stable, no freezes.
2. Single 10k-character line: wrap disabled shows horizontal scroll; enabled wraps and stays performant.

### 2.17 Error Handling
1. Simulated asset-load failure: placeholder showing and warning logged.
2. Invalid `mode`/`theme` props log warning but component stays usable.

### 2.18 CodeViewer Specific
1. Read-only viewer ignores typing/paste but allows selection/copy.
2. `onCopyRequest` (if provided) triggered when copy invoked (unit mock).

### 2.19 CodeEditor Specific
1. `onSubmitShortcut` fires on Ctrl/Cmd+Enter.
2. `commands` prop merges custom Ace commands without duplicates.

## 3. Execution Checklist
1. Run unit tests (`pnpm test:unit`) with `react-ace` mocked.
2. Run integration tests (`pnpm test:components`) covering Next screens.
3. Launch Playwright suites:
   - `pnpm test:e2e --project=chromium` (desktop)
   - `pnpm test:e2e --project=mobile` (mobile viewport)
4. Manual perf sweeps with large fixture; monitor devtools performance/timeline.
5. Accessibility audit via Playwright’s AX snapshot and manual SR check (VoiceOver/NVDA).
6. Record console/network logs to ensure Ace assets load once per mode/theme.
