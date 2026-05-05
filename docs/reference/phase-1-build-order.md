# Phase 1 Build Order

> Build in this exact order so something works after each step.

Each step is a swarm-eligible chunk. Use `swarm-coordinator` for steps with ≥2 subsystems.

## 1. Postgres wire protocol server (proxy/CLAUDE.md, internal/protocols/pgwire/)

Goal: `psql -h localhost -U testkey -d acme_bigquery -c "SELECT 1"` works (returns hardcoded `1` first; real backend later).

Files: `cmd/proxy/main.go`, `internal/protocols/pgwire/{server,protocol,session,auth}.go`.

Test: `test/integration/pg_wire_e2e_test.go` smoke against the hardcoded handler.

## 2. Control-plane DB schema + migrations (internal/store/)

Tables: `customers`, `connections`, `synced_tables`, `query_log`, `api_keys`, `sync_jobs`, `cost_attribution`. golang-migrate. sqlc query funcs.

Files: `migrations/0001_initial.up.sql` (+`.down.sql`), `internal/store/{db,crypto,queries/*}.go`.

## 3. Auth middleware (internal/protocols/pgwire/auth.go)

Validate API key from PG `user` field against `api_keys` table. Reject unknown.

## 4. BigQuery connector (internal/connectors/bigquery.go)

Forward all queries to BigQuery, stream results back. No routing yet.

Auth: service-account JSON (`docs/architecture/warehouse-auth.md`).

Goal: a real BI tool can connect through the proxy, run a SELECT against `bigquery-public-data.samples.shakespeare`, get rows back.

Test: `test/integration/bigquery_connector_test.go`.

## 5. SQL router (internal/router/)

Classify queries DDL/DML/SELECT/`ai_generate`/unsynced/stale. Phase 1: classify + log; everything still goes to warehouse.

Files: `internal/router/{classifier,table_extractor,freshness,decision}.go`.

Test: `test/integration/router_classification_test.go` (50 fixtures).

## 6. Iceberg writer (internal/iceberg/)

Default path: thin wrapper over DuckDB-Iceberg extension. Write a `samples.shakespeare` snapshot to LocalStack S3.

Test: `test/integration/sync_initial_load_test.go` + `iceberg-spec-validator` cross-read with `pyiceberg`.

## 7. BigQuery CDC sync (internal/sync/)

Two-mode: native CDC (when enabled) or `_PARTITIONTIME` watermark. Initial load via Storage Read API.

Test: `test/integration/sync_watermark_test.go` against `austin_bikeshare.bikeshare_trips`.

## 8. DuckDB worker pool (internal/workers/)

Pre-warm on customer connect. ATTACH Iceberg from LocalStack. Execute SELECT. Compare result vs direct BQ.

Test: `test/integration/duckdb_attach_test.go` (parity vs `usa_names.usa_1910_current` direct).

## 9. Full routing (internal/router/decision.go wired to workers/connectors)

DuckDB for synced+fresh reads; BigQuery for everything else. Explicit fallback with logging.

Goal: a Looker-style dashboard pointed at the proxy returns identical results, with most reads landing on DuckDB.

## 10. Snowflake connector (internal/connectors/snowflake.go)

Same as step 4 but for Snowflake. Auth: username/password + JWT-PKCS8.

Phase 1 status: connector wired, integration tests SKIP without account.

## 11. Snowflake CDC sync (internal/sync/snowflake_syncer.go)

STREAM-based polling. Apply changes via DuckDB-Iceberg extension DML.

Phase 1 status: code complete, tests SKIP without account.

## 12. `ai_generate()` rewriter (internal/ai/)

Path A (DuckDB llm extension) for ≤500 rows; Path B (batch LLM API) for >500.

Test: `test/integration/ai_generate_path_{a,b}_test.go` against `samples.shakespeare`.

## 13. REST API + frontend (internal/api/, frontend/)

Endpoints listed in `docs/reference/product-brief.md` §8. React + shadcn UI for connections, sync, queries, savings analytics, API keys.

Goal: full onboarding flow without touching SQL or env vars.

## Phase 1 done = success criteria

1. A BigQuery customer can point Looker at the proxy and all reports load.
2. ≥80% of read queries route to DuckDB (visible in dashboard).
3. `SELECT ai_generate('Classify: ' || text, 'gpt-4o-mini') FROM t LIMIT 100` works.
4. Sync lag <2 min for tables with active writes.
5. Proxy adds <100ms overhead vs direct BQ for DuckDB-routed queries.
6. Snowflake architecture proven (interface conformance + unit tests pass), Phase 2 wiring is ≤3 weeks of work.
