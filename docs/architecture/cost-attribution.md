# Cost Attribution — Per-Warehouse APIs

`internal/cost/` + `cost-attribution-backfiller` agent.

## The two numbers

- **Estimated cost:** computed at route time from query plan / bytes-scanned estimates. Synchronous. Always present.
- **Actual cost:** retrieved from the warehouse's history view. Asynchronous (15min–24h lag). Backfilled.

`saved_cost = estimated_cost - actual_cost` (when routed to DuckDB, actual = ~$0.01 of compute, so saved ≈ estimated).

## Per-warehouse APIs

### Snowflake

Real-time (last 14 days):
```sql
SELECT query_id,
       credits_used_cloud_services,
       bytes_scanned,
       warehouse_size,
       execution_time
FROM TABLE(INFORMATION_SCHEMA.QUERY_HISTORY())
WHERE query_id IN ($query_ids)
```

Historical (45min lag, full retention):
```sql
SELECT query_id, credits_used_cloud_services, ...
FROM SNOWFLAKE.ACCOUNT_USAGE.QUERY_HISTORY
WHERE query_id IN ($query_ids)
  AND start_time > $since;
```

Cost: `credits_used * customer_per_credit_rate`. Customer's per-credit rate stored in `connections.warehouse_meta` (asked during onboarding; defaults to public Snowflake list price).

### BigQuery

```sql
SELECT job_id,
       total_bytes_billed,
       total_slot_ms,
       reservation_id
FROM `region-us`.INFORMATION_SCHEMA.JOBS_BY_PROJECT
WHERE job_id IN UNNEST(@job_ids)
  AND creation_time > @since;
```

Cost computation:
- On-demand: `total_bytes_billed * $5 / TiB`.
- Reservation: `total_slot_ms / (1000 * 3600) * reservation_hourly_rate`.

Reservation rate stored in `connections.warehouse_meta`.

### Databricks (Phase 2)

```sql
SELECT *
FROM system.billing.usage
WHERE workspace_id = ?
  AND usage_metadata.job_id IN (?, ?, ...)
  AND usage_start_time > ?;
```

Cost: `usage_quantity * sku_unit_price` (joined to `system.billing.list_prices`).

## Backfill loop

`cmd/sync` runs a cron (default every 1h):
1. Pick rows where `actual_cost IS NULL` and `created_at < now() - lag(warehouse)`.
2. Group by `(customer, warehouse, query_id)`.
3. Query the warehouse's history view; join by query ID.
4. Update `actual_cost` and `saved_cost`.
5. After 24h with no match, set `actual_cost = -1` (sentinel) + alert.

Detail in `cost-attribution-backfiller` agent.

## Estimated cost computation (route time)

For warehouse-routed queries:
- Snowflake: query plan estimate via `EXPLAIN`; bytes_scanned * (1 + cloud_services_factor) * per-byte rate.
- BigQuery: dry-run via `bigquery.JobConfig{DryRun:true}`; returns `totalBytesProcessed` → multiply by on-demand rate.
- Databricks: SQL `EXPLAIN` not cost-tagged; estimate from table size + selectivity heuristic.

For DuckDB-routed queries: estimated = warehouse cost the query *would have* taken (same dry-run as above) — that's the "saved" baseline.

## UI

`/analytics/savings` shows estimated, actual, saved per day per warehouse. Hover for query-level breakdown.

## Files

| File | Purpose |
|---|---|
| `snowflake_history.go` | Pull from `INFORMATION_SCHEMA.QUERY_HISTORY` + `ACCOUNT_USAGE.QUERY_HISTORY` |
| `bigquery_jobs.go` | Pull from `INFORMATION_SCHEMA.JOBS_BY_PROJECT` |
| `databricks_usage.go` | Pull from `system.billing.usage` (Phase 2) |
| `backfiller.go` | Cron loop, batched updates |
| `estimator.go` | Route-time estimate per warehouse |
