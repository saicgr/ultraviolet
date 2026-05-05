# Testing — Pyramid + Real Warehouses

> **Hardest rule:** test against `bigquery-public-data.*` BEFORE writing any mock. The user's no-fallback rule applies to tests too.

## The pyramid

```
        ┌────────────────────┐
        │  E2E (a few)        │   psql / dbt / Looker → proxy → BQ → assert
        ├────────────────────┤
        │  Integration (lots) │   ./test/integration/*_test.go vs bigquery-public-data
        ├────────────────────┤
        │  Unit (many)        │   *_test.go in each internal/<pkg>
        └────────────────────┘
```

- **Unit tests** for pure functions (parser, classifier, type mapper). Mock only the external SDK at the package boundary.
- **Integration tests** for any code path touching a warehouse, S3, or DuckDB. Real BQ, real LocalStack S3, real DuckDB CGo.
- **E2E tests** for the full proxy flow with a real client driver.

## Integration tests against `bigquery-public-data.*`

Eight required golden tests (per scaffold plan §6):

| File | Dataset | Asserts |
|---|---|---|
| `bigquery_connector_test.go` | `samples.shakespeare` | RowDescription OIDs, DataRow encoding, cancel via job ID |
| `router_classification_test.go` | n/a | 50 fixture queries route as expected |
| `sync_initial_load_test.go` | `samples.shakespeare` | Iceberg manifest v2-conformant on LocalStack |
| `sync_watermark_test.go` | `austin_bikeshare.bikeshare_trips` | `_PARTITIONTIME` watermark, no dups |
| `duckdb_attach_test.go` | `usa_names.usa_1910_current` | byte-equal vs direct BQ |
| `ai_generate_path_a_test.go` | `samples.shakespeare` LIMIT 50 | DuckDB `llm` extension path |
| `ai_generate_path_b_test.go` | `samples.shakespeare` LIMIT 5000 | OpenAI/Anthropic batch path |
| `pg_wire_e2e_test.go` | any | psql → proxy → BQ → psql round-trip |

## BI-tool compat tests (Phase 1.5)

| Tool | Test | Pass criterion |
|---|---|---|
| dbt | tiny project, 3 models, jinja includes | `dbt run` exit 0 |
| Looker | sample explore SQL | result parity vs direct |
| Tableau | extract refresh | bulk-select succeeds |
| Hex | chained SQL cells | each cell returns expected schema |

## Local proxy port (dev + test)

`UV_PROXY_PORT=5000` in dev/test. `5432` in prod. Avoids collision with a local Postgres. All integration tests connect to `localhost:5000`. See `docs/reference/env-vars.md`.

## Setup discipline

`test/integration/setup_test.go` `TestMain`:
1. Boot LocalStack (S3) via `testcontainers-go`.
2. Boot Postgres for control plane via `testcontainers-go`.
3. Apply migrations (`golang-migrate up`).
4. Seed a fake customer + BQ connection in control plane.
5. Run tests. Tear down.

`bigquery-public-data` access requires `GOOGLE_APPLICATION_CREDENTIALS` set. Tests that need it use `testRequiresBQ(t)` helper that calls `t.Skip` with explicit reason (NEVER `PASS`) when env var is unset.

## Wire-protocol fuzzing

`test/fuzz/pg_wire_fuzz_test.go` — Go 1.18+ native fuzzing of the PG protocol parser. Run weekly in CI; corpus checked into `test/fuzz/corpus/`.

## Race detector

Always run integration + unit with `-race`. Costs 2–10x runtime, catches concurrency bugs that take days to debug in prod.

## Test naming

```go
func TestRouter_RoutesDDLToWarehouse(t *testing.T)
func TestRouter_RoutesAIGenerateUnder500ToDuckDB(t *testing.T)
func TestSyncWorker_HandlesWatermarkBoundaryNoDupes(t *testing.T)
```

Subject_Action format. Imperative present tense. The name describes what passes, not the function-under-test.

## Table-driven tests

Required for any function with >2 input variants:
```go
func TestRouter_Decision(t *testing.T) {
    cases := []struct {
        name     string
        sql      string
        expected string
    }{
        {"ddl_create_table", "CREATE TABLE t (id INT)", "warehouse"},
        {"ddl_drop_table", "DROP TABLE t", "warehouse"},
        {"select_synced_fresh", "SELECT * FROM acme.users", "duckdb"},
        {"select_unsynced", "SELECT * FROM acme.unsynced", "warehouse"},
        // ...
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) { /* ... */ })
    }
}
```

`code-organizer` agent rejects non-table-driven tests for multi-case functions.

## CI gate

`make test-integration` is **gate step 4** in the swarm-coordinator's gate sequence — must pass before any branch merges. If the test suite can't run (missing GCP creds in CI), the gate fails the merge with `MISSING_BQ_CREDS` — never silently passes.
