# Iceberg Modes — Sync, Catalog Passthrough, BYO

Per-table sync configuration picks one of three modes. Documented at `internal/sync/CLAUDE.md`. Selected on `POST /api/v1/sync/tables { mode: "sync" | "passthrough" | "byo" }`.

## Mode 1: Sync (default)

Ultraviolet polls CDC from the source warehouse, writes Iceberg files to its own object storage, DuckDB ATTACHes from there.

```
Snowflake/BQ table → CDC poll → Iceberg writer → S3 (uv-managed) → DuckDB ATTACH
```

When to use: source table is in a regular warehouse format (not natively Iceberg).

Pros: full control, transparent to customer, works for any source.
Cons: sync lag (60s default); duplicates storage cost; sync infrastructure.

## Mode 2: Catalog Passthrough

Source warehouse already exposes the table as Iceberg via a REST catalog. Ultraviolet doesn't sync; DuckDB reads the source's catalog directly.

Snowflake Horizon Catalog supports this for Snowflake-managed Iceberg tables ([Greybeam blog](https://blog.greybeam.ai/iceberg-and-snowflake/)). BigQuery's BigLake metastore supports it for BigLake-managed Iceberg tables.

```
Snowflake-managed Iceberg → Horizon Catalog REST endpoint
                              ↓
                  DuckDB ATTACH 'http://horizon/v1/...' TYPE ICEBERG, REST true
```

When to use: customer has Snowflake-managed Iceberg or BigLake Iceberg already.

Pros: zero sync lag; no duplicated storage; no sync infrastructure.
Cons: requires source to support REST catalog; auth flow more complex (catalog credentials separate from warehouse query credentials).

## Mode 3: BYO Iceberg

Customer already maintains Iceberg tables on their own S3/GCS (via Spark / Glue / dbt-iceberg). Ultraviolet just registers the location; DuckDB ATTACHes directly.

```
Customer's Iceberg on customer S3 → DuckDB ATTACH 's3://customer-bucket/...'
                                       (with cross-account IAM role)
```

When to use: data-engineering-mature customer with existing lakehouse.

Pros: simplest onboarding; lowest cost; customer keeps full data sovereignty.
Cons: customer is on the hook for catalog freshness; no UV-side cost attribution for sync (because there is no sync); UV adds value only at the routing + AI rewriter layers.

## Decision matrix

| Customer profile | Recommended mode |
|---|---|
| Mid-market analytics, no lakehouse | Sync (Mode 1) |
| Snowflake user, Iceberg-managed | Catalog Passthrough (Mode 2) |
| BigQuery user, BigLake | Catalog Passthrough (Mode 2) |
| Existing lakehouse (Spark/Trino/Dremio) | BYO (Mode 3) |
| Mixed: some tables synced, others BYO | Per-table mode selection |

## Mode interactions

- Per-table, not per-customer. A customer can have some tables in Sync mode and others in Passthrough.
- Storage mode (managed S3 vs BYOS — `storage-modes.md`) is independent: a customer in Sync mode can choose managed or BYOS storage.
- AI rewriter and routing logic are mode-agnostic — they only see DuckDB-attached tables.
