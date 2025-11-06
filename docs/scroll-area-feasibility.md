# Meltica Web UI Scroll Audit & shadcn/ui ScrollArea Feasibility

## Overview

This document captures every scrollbar encountered across the Meltica control plane (Next.js client) and evaluates where the existing overflow behavior could be migrated to the `ScrollArea` component from `@/components/ui/scroll-area` (shadcn/ui). Each section lists:

- **Current Scroll Containers** – Every element providing scroll behavior (viewport/root and nested containers).
- **Scroll Characteristics** – Dimensions, direction, and purpose.
- **ScrollArea Feasibility** – Whether swapping to `ScrollArea` is practical, with rationale and recommended next steps.

> **Legend**  
> ✅ Feasible – A drop-in or minimally invasive ScrollArea swap is likely.  
> ⚠️ Limited – Technically possible but with caveats (styling, interaction, semantics).  
> ❌ Not Recommended – ScrollArea conflicts with the component’s internal scroll management or provides no value.

---

## 1. Dashboard (`/`)

| Current Container | Dimensions | Purpose | ScrollArea Feasibility |
| --- | --- | --- | --- |
| `html` (root viewport) | `scrollHeight ~1173 px` | Global page height | ❌ No benefit; root-level scroll should stay native. |

No dialogs or nested scrollables exist on the dashboard.

---

## 2. Strategy Instances (`/instances`)

### Base Page
- `html` vertical overflow (`scrollHeight ~791 px`) – ❌ keep native.

### Create / Edit Instance Drawer (shared component)
- Drawer root: `div.flex-1.overflow-y-auto.pr-1` – wraps multistep form.
- Nested Ace editor: `div#ace-editor` with `ace_scroller` + `ace_scrollbar-v`.

**Feasibility:**  
- Drawer root → ✅ Replace with `ScrollArea` to unify overflow styling (`h-full` viewport, keep form width).  
- Ace editor scroller → ❌ Ace manages its own scrollbars; wrapping in `ScrollArea.Viewport` would double-handle wheel/keyboard events.

### Instance History Modal
- Dialog root relies on page-level scroll only when content grows; small viewport currently. No extra container → ❌ negligible gain.

---

## 3. Strategies (`/strategies`)

- Sole scroll source is `html` (`scrollHeight ~2445 px`) – ❌.

---

## 4. Strategy Modules (`/strategies/modules`)

### Main Table
- `div.relative.w-full.overflow-x-auto` wraps the table to expose horizontal scroll (`scrollWidth ~1453 px`).

**Feasibility:**  
- ✅ Wrap the table in `<ScrollArea orientation="horizontal">` (Radix supports horizontal bars).  
  - Keep a `<div className="min-w-[1450px]">` inside the viewport to preserve table width.
  - Add `<ScrollBar orientation="horizontal" />` for clarity.

### Metadata Dialog
- Dialog root: `div#radix… max-h-[85vh] overflow-y-auto`.
- Contains standard text content with natural height growth.

**Feasibility:**  
- ✅ Swap dialog body to `ScrollArea` for consistent scrollbars (vertical only). Keep `max-h` constraint.

### Source Viewer Dialog
- Same dialog root as metadata.
- Ace viewer handles code scrolling: `ace_scroller`, `ace_scrollbar-v`.

**Feasibility:**  
- Root → ✅ (vertical `ScrollArea`).  
- Ace scroller → ❌ (leave native).

### Edit Source Drawer
- Wide sheet with `overflow-y-auto`, embedding Ace editor.  
- Additional horizontal overflow from Ace when lines exceed width.

**Feasibility:**  
- Drawer root → ✅ use `ScrollArea`.  
- Ace scroller → ❌; maintain native.

### New Module Drawer & Targeted Refresh Dialog
- Similar root container (`max-h` + `overflow-y-auto`).  
- Multiple Ace editors for strategy selectors and code snippets.

**Feasibility:**  
- Roots → ✅.  
- Ace scrollers → ❌.

### Delete Confirmation
- Small confirm dialog; no overflow (native). → ❌.

---

## 5. Providers (`/providers`)

### Base Page
- `html` vertical scroll (`scrollHeight ~1171 px`) – ❌.

### Create / Edit Provider Drawers
- Drawer body: `div.flex-1.overflow-y-auto.pr-1` (vertical form).  
- No horizontal overflow.

**Feasibility:** ✅ Replace with `ScrollArea` (vertical orientation).

### Provider Details Sheet
- Sheet root: `div.flex-1.overflow-y-auto.pr-1`.  
- Nested list: `div.max-h-56.overflow-y-auto.space-y-1` showing instrument usage.

**Feasibility:**  
- Root → ✅.  
- Nested list → ✅ (convert to nested `ScrollArea` with limited height), but ensure nested Radix scrollbars look intentional.

### Stop / Start Buttons
- No dialogs triggered; only toasts update status – no change needed.

---

## 6. Adapters (`/adapters`)

- Only `html` vertical overflow (`scrollHeight ~1085 px`). → ❌.

---

## 7. Risk Limits (`/risk`)

- `html` vertical overflow (`scrollHeight ~1451 px`). → ❌.

---

## 8. Context Backup (`/context/backup`)

- Base layout: `html` vertical overflow.
- Ace viewer/editor manage their own vertical scrollbars (`ace_scroller`, `ace_scrollbar-v`), plus horizontal for the read-only viewer when JSON exceeds width.

**Feasibility:**  
- Outer card wrappers currently static; wrapping them in `ScrollArea` would not change behavior and could interfere with Ace wheel events. → ❌ (leave as-is).

---

## 9. Summary Matrix

| Area | Containers Worth Converting | Rationale |
| --- | --- | --- |
| `/instances` drawers | Drawer root only | ScrollArea can standardize radial scroll; leave Ace native. |
| `/strategies/modules` main table | Horizontal ScrollArea | Improves consistent horizontal bar styling. |
| `/strategies/modules` dialogs/drawers | Dialog sheet roots | Provide uniform scroll styling; keep Ace native. |
| `/providers` drawers | Drawer roots + usage lists | ScrollArea gives consistent experience; nested list benefits from Radix scrollbar. |
| Global root (`html/body`) | None | Native viewport scroll should remain. |
| Ace editors | None | Internal Ace scrollbars conflict with additional wrapper. |
| Static pages (`/`, `/strategies`, `/adapters`, `/risk`) | None | Only root scroll; ScrollArea adds no value. |
| Context Backup | None | Ace already optimizes scroll; wrappers don’t need ScrollArea. |

---

## Implementation Notes

1. **Import Scaffolding**
   ```tsx
   import { ScrollArea, ScrollBar } from '@/components/ui/scroll-area';
   ```

2. **Typical Pattern**
   ```tsx
   <ScrollArea className="max-h-[...]" type="auto">
     <div className="pr-4"> ...content... </div>
     <ScrollBar orientation="vertical" />
   </ScrollArea>
   ```
   - Keep the Radix `ScrollBar` visible when horizontal bars are required (`orientation="horizontal"`).

3. **Accessibility**
   - Ensure `aria-label` or `role="region"` on ScrollArea wrappers where semantic meaning matters (e.g., “Provider settings list”).
   - For drawers with nested ScrollAreas, confirm focus trapping still works (Radix Dialog + ScrollArea coexist well).

4. **Styling**
   - Preserve existing Tailwind spacing (`pr-1`, `max-h` constraints).
   - Remove old `overflow-y-auto`/`overflow-x-auto` classes when `ScrollArea` takes over to avoid redundant styling.

5. **Testing Suggestions**
   - Verify keyboard scroll (Page Up/Down, arrow keys) after conversion.
   - Confirm mouse-wheel behavior doesn’t conflict with Ace editors.
   - Check nested ScrollArea behavior (Provider details instruments list) in both light/dark themes.

---

## Key Takeaways

- The biggest wins are in drawers and sheets where Radix Dialog already governs layout; moving to `ScrollArea` standardizes scrollbars and unlocks consistent styling (particularly on macOS where native scrollbars may hide).
- Ace editors must remain responsible for their own scroll logic—wrapping them in `ScrollArea` leads to double scrolling and broken cursor behavior.
- Static pages and simple dialogs should remain on native scroll; ScrollArea is best applied where custom overflow styling is beneficial.

By targeting the feasible areas above, you can modernize the UI scrollbar experience without destabilizing Ace-based editors or root viewport behavior.
