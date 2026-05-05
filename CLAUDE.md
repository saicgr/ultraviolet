# Ultraviolet — Entry Point

> Ultraviolet = Go-based multi-warehouse query proxy. Postgres wire (Phase 1) → ADBC + Snowflake-wire (Phase 2). Routes cheap reads to managed DuckDB workers reading Iceberg on S3/GCS; forwards everything else to Snowflake / BigQuery / Databricks. Goal: 70–90% warehouse cost cut, zero BI-tool changes.

## Workflow (every non-trivial task)

1. **Read `docs/INDEX.md` first** to discover relevant topical docs by topic. Read only what's relevant.
2. **Ultrathink + enumerate ALL edge cases** before any plan. See `docs/process/ultrathink.md` (mandatory).
3. **Per-folder CLAUDE.md auto-loads** when you work in `cmd/`, `internal/<pkg>/`, `frontend/`, `migrations/`, `docs/`, `.claude/`, or `test/`. Each is the entry point for its folder.
4. **Swarm-eligible tasks delegate to `swarm-coordinator` agent.** Triggers + decomposition + gate + cleanup live in `.claude/agents/swarm-coordinator.md`. Default single-thread for trivial work; swarm for cross-stack (≥2 subsystems / ≥4 files / ≥30min).
5. **Verify cleanly before declaring done.** See `docs/process/compile-cleanly.md` (Go / TS / SQL / Bash verification commands; the import-error trap).
6. **Test against real warehouses, not mocks.** New code paths get a `test/integration/*_test.go` driven by `bigquery-public-data.*` BEFORE any unit-test mock. See `docs/process/testing.md`.
7. **No duplication.** See `docs/process/code-cleanliness.md`. Search-before-write.
8. **Append `docs/changelog/CHANGELOG.md`** for every architectural decision. Append `docs/changelog/IMPROVEMENTS.md` for every open observation.

## Conflict precedence (when docs disagree)

`docs/reference/product-brief.md` (canonical for product scope) > `docs/architecture/*` (canonical for system design) > `docs/process/*` (canonical for workflow) > `docs/conventions/*` (canonical for style) > root CLAUDE.md (canonical for entry-point routing).

## Critical-every-turn invariants (cannot defer to topical docs)

- **No mock / fallback data** — return proper Postgres error responses; never silently degrade. DuckDB→warehouse fallback is allowed *only with explicit logging*, never as silent recovery.
- **No file >1000 lines** — split into modules under `internal/<domain>/`. Target <500 for new files.
- **Plan before code, every time** — even small fixes have cascading effects.
- **Encrypt warehouse credentials at rest** (AES-256-GCM). Never log plaintext credentials or raw SQL with literals in prod — only normalized SQL.
- **Per-customer table namespacing in DuckDB** — `{customer_id}_{schema}_{table}`, no exceptions.
- **CGo enabled** for DuckDB (`marcboeker/go-duckdb`). Document M-series caveats in `docs/reference/requirements.md`.
- **Iceberg writes atomic** — commit new snapshot or rollback. Never partial state.
- **Goroutine discipline** — DuckDB execution never on the proxy main goroutine; always with `context.WithTimeout` ≤30s.
- **Test against `bigquery-public-data.*` before mocks** — the scaffold itself can't pretend integrations work.

## Lessons meta-rule

**Plan before code, every time.** The largest production incidents trace to small edits without surrounding context. Use the `swarm-coordinator` for anything cross-stack.
