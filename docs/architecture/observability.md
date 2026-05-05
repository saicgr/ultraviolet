# Observability — Query Log, Metrics, Traces

## Query log schema

Every query through the proxy logged async. Buffered channel → batched insert every 5s.

```go
type QueryLog struct {
    ID            uuid.UUID
    CustomerID    uuid.UUID
    ConnectionID  uuid.UUID
    APIKeyID      uuid.UUID
    SQL           string  // raw, encrypted at rest
    NormalizedSQL string  // literals stripped — used for grouping + dashboards
    SQLHash       string  // sha256(NormalizedSQL) — for dedupe
    RoutedTo      string  // duckdb | bigquery | snowflake | databricks | hybrid
    RouteReason   string  // codes from `routing.md`
    DurationMs    int64
    RowsReturned  int64
    BytesScanned  int64   // warehouse-reported; 0 for DuckDB
    EstimatedCost float64 // computed at route time
    ActualCost    float64 // backfilled by cost-attribution-backfiller
    SavedCost     float64 // estimated - actual
    Error         string  // SQLSTATE + message; empty on success
    CreatedAt     time.Time
}
```

`SQL` column encrypted at rest (AES-256-GCM, customer-specific subkey derived from `ENCRYPTION_KEY`). Decryption only when staff explicitly views via UI with audit log.

`NormalizedSQL` strips literals via `pg_query_go`'s normalize: `SELECT * FROM t WHERE id = 42` → `SELECT * FROM t WHERE id = $1`. Safe to log/aggregate.

## Metrics (Prometheus)

```
uv_proxy_connections_active{customer_id}             gauge
uv_proxy_query_total{customer_id, routed_to, status} counter
uv_proxy_query_duration_seconds{routed_to}           histogram
uv_router_decision_total{routed_to, reason}          counter
uv_router_fallback_total{from, to, reason}           counter
uv_workers_pool_size{customer_id}                    gauge
uv_workers_checkout_wait_seconds                     histogram
uv_sync_lag_seconds{customer_id, table}              gauge
uv_sync_backpressure_total{customer_id}              counter
uv_iceberg_snapshot_commit_seconds                   histogram
uv_ai_generate_calls_total{provider, model, path}    counter
uv_ai_generate_tokens_total{provider, model, kind}   counter (kind=in|out)
uv_cost_estimated_usd_total{customer_id, warehouse}  counter
uv_cost_actual_usd_total{customer_id, warehouse}     counter
```

## Traces (OpenTelemetry)

Spans per query: `proxy.handle_query` → `router.decide` → `workers.execute` OR `connectors.snowflake.execute` → `result.stream`. Trace ID propagated to warehouse via SDK comment (`/* uv_trace_id=abc123 */`) when supported.

## Log format (zerolog, JSON)

```json
{"level":"info","time":"2026-05-05T...","customer":"acme","route":"duckdb","duration_ms":42,"rows":150,"sql_hash":"7af0...","msg":"query.completed"}
```

PII rules: never log raw SQL in prod (only `sql_hash`); never log credentials, API keys, customer-name fragments unrelated to the customer ID.

## Dashboards (frontend)

`/queries` — last 1000 queries, filterable.
`/analytics/savings` — daily estimated vs actual; saved % and $.
`/analytics/routing` — % routed to DuckDB; fallback rate; per-table sync lag.

## Alerting (Phase 1.5)

- `uv_router_fallback_total` > 5%/hr → warn (DuckDB instability).
- `uv_sync_lag_seconds` > 10 min for >15 min → page (sync stuck).
- `uv_ai_generate_calls_total` provider error rate > 10% → warn.
- Postgres-side: `query_log` insert lag > 30s → page (channel backed up).
