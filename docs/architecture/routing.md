# Query Routing — Decision Tree

Implemented in `internal/router/decision.go`. Order is canonical — do not reorder.

## Decision rules

```
parse SQL with pg_query_go
   │
   ▼
1. Is this a DDL? (CREATE / DROP / ALTER / TRUNCATE / RENAME)
       YES → warehouse (always)
       NO  → continue
   │
   ▼
2. Is this a write? (INSERT / UPDATE / DELETE / MERGE / COPY FROM)
       YES → warehouse (always)
       NO  → continue
   │
   ▼
3. Does the SQL contain `ai_generate(...)`?
       YES → invoke internal/ai/rewriter
             rewrite to Path A (DuckDB llm extension) if estimated <500 rows
             rewrite to Path B (batch LLM API) if ≥500 rows
             route to DuckDB worker (Path A) or proxy-side executor (Path B)
       NO  → continue
   │
   ▼
4. Extract referenced tables. For each table:
       Is it in the customer's synced_tables map?
           NO → warehouse (table not available in DuckDB)
           YES → check freshness
   │
   ▼
5. For each referenced synced table:
       Is the Iceberg snapshot stale (> per-table max_lag, default 5 min)?
           YES → warehouse
           NO  → continue
   │
   ▼
6. Hybrid pushdown candidate? (Phase 2 — Phase 1 just logs)
       Query touches synced + unsynced tables, OR
       Query has selective filter that could push to warehouse
           → log `routing.pushdown.candidate`
           → Phase 1: route to warehouse anyway
           → Phase 2: split query, hybrid execute
   │
   ▼
7. Default → DuckDB worker (the goal — 80%+ of queries land here)
```

## Reason codes (logged on every decision)

| Code | Meaning |
|---|---|
| `ddl` | DDL statement |
| `write` | DML write |
| `ai_path_a` | `ai_generate` ≤500 rows, DuckDB llm extension |
| `ai_path_b` | `ai_generate` >500 rows, batch LLM |
| `unsynced_table=<name>` | referenced table not in sync config |
| `stale_snapshot=<name>` | snapshot lag exceeds max |
| `pushdown_candidate` | Phase 2 hybrid candidate (currently routes to warehouse) |
| `duckdb` | default cache hit |
| `fallback` | DuckDB worker errored or timed out — explicit warehouse fallback |

## Fallback discipline (no silent degrade)

If a DuckDB worker errors or exceeds 30s:
1. Log `routing.fallback.warehouse reason=duckdb_error error=<msg>`.
2. Re-execute on warehouse, return result.
3. Increment `uv_router_fallback_total` Prometheus counter.
4. **Never hide this from the operator.** The user gets a slower result but a valid one; the metric tells the operator something is wrong.

Per project invariant: **no silent fallback paths.** Every fallback emits a structured log line with full context.

## Stale freshness check

`internal/router/freshness.go` keeps a Redis-backed map: `customer:{id}:table:{name} -> last_synced_at_ts`. Updated by `internal/sync` via Redis pubsub on every snapshot commit. Read on hot path, not Postgres-queried.

## SQL parsing

Use `github.com/pganalyze/pg_query_go/v5` (real PG parser via CGo). Never roll a regex. AST walk extracts referenced tables (`extractTablesFromAST`) including CTEs, subqueries, JOINs.
