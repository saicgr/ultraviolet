# Ultraviolet Frontend Design — shadcn / Modern Data UI

Source of truth for the design system: this file + `vercel:shadcn` skill. Use when generating UI code in `frontend/`.

---

## 1. Visual identity

- **Brand mark:** Ultraviolet — wordmark in `ui-violet-600`.
- **Primary accent:** ultraviolet `#7C3AED` (Tailwind `violet-600`).
- **Secondary accent:** electric cyan `#06B6D4` (Tailwind `cyan-500`) — for "saved cost" deltas / positive status.
- **Mood:** technical, data-dense, calm. Think Linear, Cursor, Statsig — not consumer-friendly Reppora-glass.
- **Visual language:** flat. No glassmorphism. Subtle borders, generous whitespace, monospace numbers.

---

## 2. Type scale (Tailwind tokens)

| Use | Class | Example |
|---|---|---|
| Page title | `text-2xl font-semibold tracking-tight` | "Query Analytics" |
| Section heading | `text-lg font-medium` | "Last 24 hours" |
| Body | `text-sm text-foreground` | regular copy |
| Helper | `text-xs text-muted-foreground` | timestamps, breadcrumb |
| Numbers / IDs | `font-mono tabular-nums` | savings $, query ID, latency |

Never mix font-mono and font-sans on the same number. Always `tabular-nums` on tables.

---

## 3. Color tokens (CSS vars from shadcn theme)

Use semantic tokens, not raw hex. Tailwind config exposes:
- `bg-background` / `bg-card` / `bg-muted`
- `text-foreground` / `text-muted-foreground`
- `border-border`
- `text-primary` (violet-600) / `bg-primary`
- `text-success` (cyan-500) for positive cost deltas
- `text-destructive` (red-500) for errors
- `text-warning` (amber-500) for stale data

Dark mode is the default. Light mode supported but secondary.

---

## 4. Component primitives (shadcn only)

Install via CLI: `npx shadcn@latest add <name>`.

Allowed: `button`, `card`, `dialog`, `dropdown-menu`, `form`, `input`, `label`, `select`, `separator`, `sheet`, `skeleton`, `table`, `tabs`, `toast`, `tooltip`, `command`, `popover`, `badge`, `progress`, `alert`, `alert-dialog`, `breadcrumb`, `switch`, `textarea`, `data-table` (TanStack), `chart` (Recharts wrapper).

NOT allowed without RFC: anything outside shadcn registry (no Material, no Chakra, no Mantine).

---

## 5. Layout

- **Top nav** (sticky, `bg-background/95 backdrop-blur` allowed here only).
- **Left sidebar** (≤240px, collapsible to icon rail).
- **Main pane** with `max-w-screen-2xl mx-auto px-6 py-8`.
- **Right detail drawer** (`Sheet`) for query-detail, connection-detail.

---

## 6. Tables (the most important component)

`/queries` and `/sync/tables` are the most-viewed pages — design for dense, scannable rows.

- Use `@tanstack/react-table` + shadcn `data-table`.
- Row height 36px; padding `px-3 py-2`.
- Right-align numeric columns; `tabular-nums` for line-up.
- Sticky header row on scroll.
- Click row → opens right drawer with detail; never navigate away from list.
- Bulk-select via leading checkbox column.
- Filters in a `Popover` triggered from the header bar.

---

## 7. Numbers + units

- **Money:** `$1,234.56` (USD always). Use `Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' })`.
- **Bytes scanned:** `1.2 GiB` (binary, not decimal). Component `<Bytes value={n} />`.
- **Duration:** `42 ms` / `1.2 s` / `2 min 13 s` — never `0.00204833s`.
- **Percentages:** `87%` (no decimal unless <10%).
- **Cost saved deltas:** `+$420.13` in `text-success`, `-$12.04` in `text-destructive`. Always sign-prefixed.

---

## 8. Empty / loading / error states (mandatory)

Every async page renders one of four states — never `null`:

- **Loading:** shadcn `Skeleton` matching the eventual shape (NOT a spinner).
- **Empty:** `Card` with icon, title, body, and primary CTA. E.g., `/connections` empty: "No warehouses connected — Add your first connection".
- **Error:** `Alert variant="destructive"` with message + retry button + request ID for support.
- **Data:** the actual content.

The `ui-ux-reviewer` agent rejects PRs missing any of the four for an async page.

---

## 9. Iconography

Lucide via shadcn. Stroke 1.5. Size matches text (`size-4` next to `text-sm`, `size-5` next to `text-base`).

---

## 10. Motion

Subtle. `transition-colors` + `transition-opacity` only. No spring animations on data rows. Drawer slides in 200ms `ease-out`. Toast fades 150ms.

Reduced-motion respected via `motion-reduce:` Tailwind variants.

---

## 11. Accessibility floor

WCAG 2.2 AA. See `ui-ux-reviewer` agent §4.
