# React Ace Upgrade Plan

## Background & Goals
- Replace all code-oriented `<Textarea>` usages with React Ace–based components to deliver consistent editing and viewing experiences across the client app.
- Build two canonical wrappers (editor and viewer) so features can share configuration, language tooling, and responsive behavior.
- Ensure dialogs and cards that currently host textareas resize gracefully around Ace, with dedicated handling for small screens.
- Maintain or improve keyboard accessibility, diagnostic surfacing, and submission shortcuts.

## Current Inventory (Jan 2025)

| Location | Purpose | Container Constraints | Notes |
| --- | --- | --- | --- |
| `src/components/strategy-module-editor.tsx:75` | Strategy module editing fallback when `useEnhancedEditor` is false. | Inline component, parent expects resize handle and Ctrl/Cmd+Enter submit. | Already dynamically loads `react-ace` for enhanced mode; fallback should move to shared Ace wrapper. |
| `src/app/context/backup/page.tsx:332` | Read-only JSON export preview. | Inside `CardContent`; fixed height (`h-64`). | Needs viewer wrapper with copy-safe display and responsive height. |
| `src/app/context/backup/page.tsx:353` | Editable JSON import field with diagnostics. | Same card; currently `h-48`. | Should reuse editor wrapper with validation hook integration. |
| `src/app/strategies/modules/page.tsx:2150` | Targeted refresh dialog – selectors list entry. | `DialogContent` sized `w-full max-w-2xl`; grid layout. | Requires plain-text editor configuration with wrapping. |
| `src/app/strategies/modules/page.tsx:2163` | Targeted refresh dialog – hashes list entry. | Same dialog/grid. | Same configuration as selectors. |
| `src/app/strategies/modules/page.tsx:2555` | Read-only strategy source viewer. | Dialog `max-w-4xl max-h-[85vh] overflow-y-auto`. | Replace with Ace viewer controlling scroll via editor. |
| `tests/strategy-module-editor.spec.tsx` | Asserts textarea fallback rendering. | Unit test snapshot. | Needs update to expect new editor wrapper behavior. |

No other raw `<textarea>` elements exist; incidental form textareas are routed through `src/components/ui/textarea.tsx` which remains available for non-code forms.

## React Ace Research Snapshot
- Context7 docs confirm current major line at v14 (project package.json already pins `react-ace@^14.0.1` with `ace-builds@^1.43.4`).
- Official demo (`https://securingsincity.github.io/react-ace/`) provides reference options for mode, theme, font sizing, gutters, autocompletion, and mobile menu support.
- v14 ships ESM-first modules; dynamic `next/dynamic` with `{ ssr: false }` remains the recommended loading strategy for Next.js.
- Language/tooling imports still come from `ace-builds/src-noconflict/*`; bundle size is best managed by importing only the modes/themes in use.

## Proposed Architecture

### 1. Shared Dynamic Loader
- Create `src/components/code/ace-loader.ts` (client-safe) exporting `loadAceAssets({ mode, theme, additionalModules })`.
- Internally memoize `Promise.all([...])` per mode/theme pair to avoid duplicate imports; include core modules such as `ext-language_tools` only when needed.

### 2. `CodeEditor` Component
- File: `src/components/code/code-editor.tsx`.
- Client component using `next/dynamic` to load `react-ace` once and re-exported default `AceEditor`.
- Props (subset): `value`, `onChange`, `mode` (default `"javascript"`), `theme` (default `"tomorrow"`), `readOnly`, `disabled`, `minLines`, `maxLines`, `wrapEnabled`, `fontSize`, `lineHeight`, `placeholder`, `onSubmitShortcut`, `ariaLabel`, `height`, `className`.
- Use `useEffect` to call `loadAceAssets` according to requested mode/theme and optional extras (snippets, autocomplete).
- Provide `commands` prop hooking to Ctrl/Cmd+Enter when `onSubmitShortcut` exists.
- Enforce responsive sizing with `style={{ width: '100%' }}` and class names allowing `min-h`/`max-h` tailwind overrides.
- Expose optional `diagnostics` prop to render Ace `annotations`.

### 3. `CodeViewer` Component
- File: `src/components/code/code-viewer.tsx`.
- Wraps `CodeEditor` but forces `readOnly`, disables cursor updates, hides print margin, and optionally hides gutter (configurable).
- Accepts `value`, `mode`, `theme`, `wrapEnabled`, `ariaLabel`, `height`, `className`.
- Provide copy affordance hook via optional `onCopyRequest`.

### 4. Styling & Responsiveness
- Standardize default `fontSize=14`, `lineHeight=20` for readability while allowing overrides.
- Provide Tailwind helper classes (`min-h-[320px]`, `max-h-[60vh]`, etc.) through `className` composition rather than inline Ace height to keep responsive breakpoints.
- For dialogs, rely on `w-[min(95vw,theme(maxWidth))]` and `max-h-[80vh]` while letting Ace manage internal scroll (`setOptions={{ wrap: true }}`).

### 5. Accessibility
- Forward refs to Ace container (wrap `AceEditor` with `React.forwardRef`) so parent forms can focus programmatically.
- Preserve `aria-label` forwarding and ensure `aria-invalid` semantics via `className`/props hooking to Ace container.
- Keep keyboard shortcuts (submit, copy) documented and optionally configurable.

## Migration Steps
1. **Scaffold Code Directory**
   - Create `src/components/code/` folder with loader, editor, and viewer modules plus index barrel.
   - Add `__tests__/code-editor.test.tsx` (or update existing test suite) using jest/vitest-compatible mocks for `react-ace`.
2. **Implement Shared Loader**
   - Handle asset caching via `Map<string, Promise<void>>`.
   - Support at least the following modes: `javascript`, `json`, `text` (for selectors/hashes); themes: `tomorrow`, `github`, `monokai`.
3. **Build `CodeEditor`**
   - Wrap `AceEditor` dynamic import.
   - Apply default `setOptions`: `{ useWorker: false, wrap: true, tabSize: 2, enableBasicAutocompletion: false, enableLiveAutocompletion: false, showLineNumbers: true }`.
   - Allow consumer overrides through shallow merge.
4. **Build `CodeViewer`**
   - Compose `CodeEditor` with `readOnly`, `highlightActiveLine=false`, `showGutter` toggled via prop.
   - Optionally add `CopyButton` slot for reuse in dialogs.
5. **Refactor Strategy Module Editor**
   - Replace textarea fallback with `CodeEditor` configured for `mode="javascript"`.
   - Collapse `useEnhancedEditor` flag into Ace option toggles (e.g., fallback can disable autocomplete but still use Ace).
   - Maintain diagnostics via `annotations` prop.
   - Update keyboard shortcut logic to pass `onSubmitShortcut`.
   - Remove `Textarea` import; adjust tests to expect Ace wrapper (mock dynamic import for SSR).
6. **Refactor Context Backup Page**
   - Export card preview -> `CodeViewer` with `mode="json"`, `wrapEnabled`, `minLines` approximating 20 (matching `h-64`).
   - Import field -> `CodeEditor` with `mode="json"`, `maxLines={40}`, `onChange` bridging to existing handler, maintain placeholder.
   - Ensure surrounding layout uses `flex-1` to let Ace grow; adjust for mobile by adding `max-h-[60vh]` on small screens.
7. **Refactor Targeted Refresh Dialog**
   - Swap both textareas for `CodeEditor` with `mode="text"`, `wrapEnabled`, `minLines={6}`, `maxLines={18}`.
   - Increase dialog width to `max-w-3xl md:w-[90vw]`, set `max-h-[80vh]`, wrap content in `scroll-area` if necessary.
8. **Refactor Strategy Source Viewer**
   - Use `CodeViewer` `mode="javascript"` with `minLines={20}`, `maxLines={80}`, `wrapEnabled`.
   - Simplify dialog content to rely on Ace for scrolling; remove `overflow-y-auto` on `DialogContent`, instead set `className="max-w-5xl w-[95vw]"` and wrap Ace in responsive container.
9. **Cleanup**
   - Remove unused `Textarea` component import where no longer required.
   - Document new components in README/QUICKSTART if needed.
10. **Testing & Validation**
    - Update `tests/strategy-module-editor.spec.tsx` to mock `CodeEditor`.
    - Add regression tests for `CodeEditor` verifying submit shortcut and annotation mapping.
    - Run `pnpm lint` and `pnpm test`.
    - Manual QA: verify dialogs on small viewport (≤768px), check copy actions, confirm that content wraps without horizontal scroll.

## Risk & Mitigation
- **Bundle Size Growth**: limit mode/theme imports per usage; consider lazy-loading additional modes only when their dialogs open.
- **SSR Mismatch**: ensure all Ace consumers are client components (`'use client'`) and dynamic import disables SSR.
- **Accessibility Regression**: validate focus trap within dialogs after Ace insertion; add e2e check if possible.
- **Clipboard Behavior**: `CodeViewer` should preserve newline formatting; use `navigator.clipboard.writeText` with fallback.
- **Testing Complexity**: stub `react-ace` in unit tests to avoid jsdom issues; document approach in tests folder.

## Timeline & Ownership
- **Day 1**: Implement shared loader + `CodeEditor`/`CodeViewer` with unit tests.
- **Day 2**: Migrate `StrategyModuleEditor` and update specs; run lint/test.
- **Day 3**: Migrate context backup + targeted refresh dialog; adjust responsive styles.
- **Day 4**: Migrate source viewer dialog, perform QA sweep, finalize docs.

## Follow-Ups
- Evaluate using Ace diff/split components for future multi-pane comparisons.
- Consider centralized theme/mode configuration to avoid hard-coded strings across features.
- Track telemetry for code editor usage if analytics are available (future work).
