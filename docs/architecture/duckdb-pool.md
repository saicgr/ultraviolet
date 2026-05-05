# DuckDB Worker Pool

`internal/workers/`. CGo bindings via `github.com/marcboeker/go-duckdb`.

## Pool topology

- **Per-customer pool.** Default 3 workers/customer (configurable per-customer via control plane).
- **Pre-warm on first connection.** When the proxy receives a startup message for `acme_bigquery`, it asynchronously spins up the customer's pool and ATTACHes synced tables. First query waits if pool isn't ready (with 5s deadline; if exceeded, fall back to warehouse).
- **Idle eviction.** A pool with no checkouts for 30 min closes its workers.
- **Hard cap on total workers per process** (`UV_MAX_DUCKDB_WORKERS`, default 64). When at cap, oldest-idle pool is evicted.

## Worker lifecycle

```
created → ATTACH iceberg tables → idle → checkout → execute → checkin → idle
                                                                      ↓
                                                            CHECKPOINT + re-ATTACH
                                                            (on Iceberg refresh event)
                                                                      ↓
                                                                    idle
```

## Iceberg ATTACH syntax

S3 (managed mode):
```sql
ATTACH 's3://uv-data/{customer_id}/{table_name}/'
  AS {customer_id}_{schema}_{table_name}
  (TYPE ICEBERG, ENDPOINT_URL 's3.amazonaws.com');
```

GCS (BigQuery customers in BYOS or BigLake):
```sql
ATTACH 'gs://{customer_bucket}/{table_name}/'
  AS {customer_id}_{schema}_{table_name}
  (TYPE ICEBERG);
```

REST catalog (catalog-passthrough mode — `iceberg-modes.md`):
```sql
ATTACH 'http://localhost:8181/v1/{prefix}'
  AS {customer_id}_catalog
  (TYPE ICEBERG, REST 'true');
```

## Per-customer namespacing

DuckDB attached names: `{customer_id}_{schema}_{table_name}`. Never share names across customers — defense against any router bug that might leak a query into the wrong worker.

The router rewrites referenced tables in incoming SQL from `schema.table` → `{customer_id}_{schema}_{table_name}` before sending to DuckDB.

## Refresh on Iceberg snapshot pubsub

On Redis event `customer:{id}:table:{name}:refreshed:{snapshot_id}`:
1. The customer's pool subscribes during pre-warm.
2. On event: each worker runs `CHECKPOINT` then re-`ATTACH` with the same name (DuckDB idempotent).
3. In-flight queries on the worker are NOT interrupted; refresh applies to the next checkout.

## Hard timeout

Every checkout runs under `context.WithTimeout(ctx, 30*time.Second)` (configurable). On timeout:
1. Cancel the context.
2. Call DuckDB's `Interrupt()` on the connection.
3. Log `worker.timeout`.
4. Router falls back to warehouse (with explicit log per `routing.md`).
5. Worker returned to pool only after cleanup completes; if cleanup fails, worker is closed and pool re-fills.

## CGo + M-series caveat

`marcboeker/go-duckdb` requires CGo. On Apple Silicon, ensure `CGO_ENABLED=1` and `CC=clang`. Build flags documented in `docs/reference/requirements.md`. The `Makefile` `build` target sets these explicitly.

## Files

| File | Purpose |
|---|---|
| `pool.go` | Pool struct, checkout/checkin, eviction |
| `worker.go` | Single-worker lifecycle, CGo handle |
| `attach.go` | ATTACH SQL generation per storage mode |
| `executor.go` | Run SQL on worker; stream results to PG DataRow |
| `refresh.go` | Redis pubsub subscriber, CHECKPOINT trigger |
