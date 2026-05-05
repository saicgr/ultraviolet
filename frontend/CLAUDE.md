# `frontend/` — Control Plane UI

Stack: React 18 + TypeScript + Vite + Tailwind + shadcn/ui. Design tokens in `.claude/commands/frontend-design.md`.

## Discipline

- **shadcn primitives only** — no Material/Chakra. Theme via Tailwind config, not CSS-in-JS.
- **API calls via generated client** from the OpenAPI spec (Phase 1.5). Phase 1: hand-typed.
- **No global state libraries** — TanStack Query for server state, React Context for auth/theme. Add Zustand only if a real cross-tree need emerges.
- **`tsc --noEmit` clean before commit.** `compile-checker` agent enforces.
- **Vite dev server proxies `/api/v1/*`** to `localhost:8080` in dev.

## Pages (Phase 1)

- `/` — savings dashboard
- `/connections` — warehouse connection CRUD
- `/sync` — table sync config
- `/queries` — query log + per-query detail
- `/api-keys` — API key mgmt
- `/connection-string` — copy-paste connect string for psql / dbt

See `vercel:shadcn` skill for component conventions.
