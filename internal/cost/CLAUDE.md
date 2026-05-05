# `internal/cost/` — Cost Attribution

Canonical: `docs/architecture/cost-attribution.md`.

## Per-warehouse cost APIs

- **Snowflake:** `INFORMATION_SCHEMA.QUERY_HISTORY` (real-time, 14d) + `ACCOUNT_USAGE.QUERY_HISTORY` (45min lag, full).
- **BigQuery:** `INFORMATION_SCHEMA.JOBS_BY_PROJECT` (`bytes_billed × $5/TiB` on-demand, or slot-ms × reservation).
- **Databricks:** `system.billing.usage` (DBU × tier rate). Phase 2.

## Discipline

- **Backfill nightly** via `cost-attribution-backfiller` agent / `cmd/sync` cron job.
- **Estimated vs actual.** Router logs `estimated_cost` synchronously; backfiller fills `actual_cost` async. `saved_cost = estimated - actual`.
- **No PII in cost rows.** Reference query by ID only; SQL stays in `query_log`.

## Files

`snowflake_history.go` · `bigquery_jobs.go` · `databricks_usage.go` · `backfiller.go`.
