# CDC Sync — Per-Warehouse Strategies

`internal/sync/`. Each warehouse has its own poller; all write to Iceberg via `internal/iceberg/`.

## Snowflake — STREAM objects

Setup (run once when customer adds table):
```sql
CREATE STREAM uv_stream_{table_name} ON TABLE {schema}.{table_name};
```

Per-poll (default every 60s):
```sql
SELECT *, METADATA$ACTION, METADATA$ISUPDATE, METADATA$ROW_ID
FROM uv_stream_{table_name};
```

Apply:
- `INSERT`: append Parquet files to Iceberg.
- `DELETE` / pre-image of `UPDATE`: write deletion vectors (Iceberg v2 positional deletes).
- `UPDATE` post-image: append.

Stream consumed atomically: in Snowflake, `SELECT FROM stream` advances the offset only when the consuming statement commits. Wrap each poll in a Snowflake transaction; commit only after Iceberg snapshot commits.

## BigQuery — Two-mode CDC

### Mode 1: native CDC (`APPENDS()`)
Available for tables with row-level change tracking enabled (`ALTER TABLE ... SET OPTIONS (enable_change_history=TRUE)`).
```sql
SELECT * FROM APPENDS(TABLE `project.dataset.table`,
                     TIMESTAMP('{last_sync_time}'),
                     CURRENT_TIMESTAMP());
```

### Mode 2: timestamp watermark (fallback)
For tables without CDC enabled (most public datasets, many production tables):
```sql
SELECT * FROM `project.dataset.table`
WHERE _PARTITIONTIME > TIMESTAMP('{last_sync_time}')
   OR (last_modified_time > TIMESTAMP('{last_sync_time}'));
```

Picks watermark column in this order: `_PARTITIONTIME` (partitioned tables) > `last_modified_time` (audit columns) > `created_at`. If none, **first sync only does a full reload; subsequent syncs cannot incremental** — flag in UI as "snapshot mode" (full reload at configurable interval).

### Initial load
Always a full `SELECT *` streamed via BigQuery Storage Read API (Arrow IPC) for high throughput. Batched 100k rows → write Parquet → commit Iceberg snapshot. For tables >100GB, surface progress to UI.

## Databricks — Phase 2

Tail the Delta transaction log via Databricks SDK or direct S3/ABFS log read. Each `commit-N.json` is a transaction; convert add/remove actions to Iceberg add/delete file actions. Specifics deferred to Phase 2.

## Watermark persistence

Per-table watermark stored in control-plane DB (`synced_tables.last_sync_time`). Updated atomically with the corresponding Iceberg snapshot commit (transaction across Postgres + Iceberg requires care — prefer a 2-phase approach: write Iceberg snapshot first, then update Postgres; if Postgres update fails, retry — Iceberg writes are idempotent by snapshot ID).

## Backpressure

If Iceberg writer falls behind (queue depth > N) for a customer:
1. Mark all that customer's tables stale in Redis (`stale=true` flag).
2. Router routes those tables' queries to warehouse until catch-up.
3. Alert on `uv_sync_backpressure_total`.

## Initial-sync UI flow

1. User adds table to sync.
2. `internal/api/sync.go` validates connection + permission to read.
3. Job enqueued in `sync_jobs` table.
4. `cmd/sync` worker picks up job, runs initial load, commits first snapshot.
5. Switches to incremental mode; UI shows "Last synced X seconds ago."

## Discipline

- **Transactional commit per snapshot.** Never partial Iceberg state.
- **Idempotent on retry.** Crashing mid-sync must not double-write rows.
- **No silent fallback to full reload.** If incremental fails, mark stale + alert; full reload is an explicit manual action.
