# UI Token & Galaxy Component Alignment

Updated: 2025-11-09

## Border Radius Standard

- **Token**: `rounded-2xl` (Tailwind ⇒ 1.5rem ≈ 24px)
- **Applies to**: Table containers + headers, mobile cards, dialog shells.
- **Rationale**: Matches the softened rectangles in our existing cards while keeping enough curvature to distinguish interactive surfaces.

## Gradient / Fill Tokens

| Use case | Token | Notes |
| --- | --- | --- |
| Primary buttons | `bg-[linear-gradient(135deg,theme(colors.sky.500),theme(colors.violet.500),theme(colors.fuchsia.500))]` | Keeps previous prism look but now paired with solid fallback layer to avoid white halos. |
| Secondary cards / list rows | `bg-[linear-gradient(139deg,#242832,#241C28)]` with `text-slate-100` | Inspired by Galaxy `Cards/Na3ar-17_terrible-gecko-91`. Provides Android-style list view aesthetic on mobile. |
| Checkbox fills | `bg-[linear-gradient(135deg,theme(colors.sky.500/.95),theme(colors.indigo.500))]` with inset border | Mirrors Galaxy `Checkboxes/mrhyddenn_slippery-pug-63` while staying Tailwind-compatible. |

## Galaxy References (Context7)

- **Responsive list/card**: uiverse-io/galaxy → `Cards/Na3ar-17_terrible-gecko-91` (Context7 lookup). We ported the stacked list feel—rounded corners, linear gradient background, hover elevation—to our mobile card rows for tables.
- **Checkbox styling**: uiverse-io/galaxy → `Checkboxes/mrhyddenn_slippery-pug-63`. Provides the filled gradient background and checkmark animation we adapt via Tailwind utilities.

## Implementation Notes

1. Table headers inherit the container radius via utility classes so screenshots no longer show mismatched chrome.
2. Mobile breakpoints (`max-width: theme('screens.sm')`) now switch from tables to Galaxy-inspired cards to mimic Android list/recyclerview layouts.
3. Documentation and QA artifacts (docs/ui/responsive-matrix, docs/ui/galaxy-components.md) should reference this file whenever new components adopt these tokens.
