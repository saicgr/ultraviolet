# `internal/sync/` — CDC Sync Workers

Canonical: `docs/architecture/cdc-sync.md` + `docs/architecture/iceberg-modes.md`.

## Three sync modes per table

1. **Sync** (default) — CDC poll → write Iceberg → publish refresh.
2. **Catalog passthrough** — Snowflake Horizon / BigLake REST catalog. No write step.
3. **BYO Iceberg** — customer's own Iceberg. No write step; just register location.

## Per-warehouse CDC strategy

- **Snowflake:** `CREATE STREAM` on source table, poll every 60s default.
- **BigQuery:** `APPENDS()` table function if CDC enabled, else `_PARTITIONTIME` watermark, else full reload (initial sync only).
- **Databricks:** Delta log tail (Phase 2).

## Invariants

- **Atomic snapshot commit.** Never partial Iceberg state.
- **Backpressure aware.** If Iceberg writer falls behind, mark table stale and let router send queries to warehouse.
- **Per-table watermark in control-plane DB.** Recoverable across worker restarts.
- **Publish `{customer_id}:{table}:refreshed:{snapshot_id}` to Redis** so DuckDB workers `CHECKPOINT` + re-attach.

Iceberg writes prefer the DuckDB-Iceberg extension's v2 DML (`INSERT/UPDATE/DELETE`) over hand-rolled Parquet — see `docs/architecture/iceberg.md` §writer-strategy.
