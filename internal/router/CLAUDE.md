# `internal/router/` — Query Routing Decision

Canonical: `docs/architecture/routing.md`.

## Routing rule order (canonical — do not reorder)

1. DDL (CREATE / DROP / ALTER / TRUNCATE) → warehouse
2. Write (INSERT / UPDATE / DELETE / MERGE) → warehouse
3. Contains `ai_generate(` → rewrite (`internal/ai/rewriter.go`) then DuckDB
4. References unsynced table → warehouse
5. Synced but stale Iceberg snapshot (>N min, per-table config) → warehouse
6. Hybrid pushdown candidate (Phase 2) → log only in Phase 1
7. Default → DuckDB worker

## Discipline

- **SQL parsing via `pganalyze/pg_query_go`** — never roll a regex parser.
- **Stale freshness map in Redis pub/sub** — never query Postgres on the hot path.
- **Log every decision** with reason code. Powers the `/api/v1/analytics/routing` dashboard.
- **No silent fallback.** DuckDB worker error or >30s ⇒ explicit log line `routing.fallback.warehouse reason=<...>`, then route to warehouse. The user sees the slower result, not an error.

## Files

`classifier.go` · `table_extractor.go` · `freshness.go` · `decision.go` · `pushdown.go` (Phase 2 stub).
