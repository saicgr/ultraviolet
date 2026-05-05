# Multi-Warehouse — Connector Interface

`internal/connectors/`. Strategy: **build for 3, launch with 1.**

## Connector interface

```go
package connectors

type Connector interface {
    // Execute streams rows back as ResultSet. ctx-cancellable.
    Execute(ctx context.Context, sql string) (ResultSet, error)

    // Cancel stops a query by warehouse-side query ID.
    Cancel(ctx context.Context, queryID string) error

    // Introspect lists tables + columns + partition specs.
    Introspect(ctx context.Context) ([]Table, error)

    // CostHistory fetches actual cost for queries in a time range.
    CostHistory(ctx context.Context, since time.Time) ([]QueryCost, error)

    Close() error
}

type ResultSet interface {
    Schema() []Column          // Postgres OID + name
    Next(ctx context.Context) (Row, error)  // io.EOF when done
    QueryID() string           // warehouse query ID for cancel + cost lookup
    Close() error
}

type Column struct {
    Name string
    OID  uint32  // Postgres type OID per skill `pg-wire-reference`
    Mode string  // "nullable" | "required" | "repeated"
}
```

## Phase 1 implementations

| Warehouse | File | SDK | Phase 1 status |
|---|---|---|---|
| BigQuery | `bigquery.go` | `cloud.google.com/go/bigquery` | Fully wired (launch warehouse) |
| Snowflake | `snowflake.go` | `github.com/snowflakedb/gosnowflake` | Connector wired, integration tests skipped without account |
| Databricks | `databricks.go` | `github.com/databricks/databricks-sql-go` | Stub returning `feature_not_supported` |

Each implementation:
- Connection pool per customer (max 5 connections).
- Streams large results (BQ Storage Read API; Snowflake Arrow batches).
- Maps warehouse types to Postgres OIDs via `result.go`.
- Maps warehouse errors to Postgres SQLSTATE per `docs/conventions/error-mapping.md`.

## Per-warehouse auth

See `warehouse-auth.md` for the full matrix. Connector constructor takes a `Credential` interface; concrete types per warehouse.

## "Build for 3, launch with 1"

Phase 1 ships BigQuery as the only end-to-end-tested warehouse. Snowflake and Databricks connector code exists, has the same interface, has unit tests against mocked SDK responses, but isn't claimed in marketing until the integration test suite passes against a real Snowflake / Databricks account.

This means:
- The codebase is multi-warehouse-ready from day one (not a rewrite later).
- Phase 1 launch is focused and production-quality (BQ).
- Adding Snowflake (Phase 2) is wiring + testing, not architecture.
- Honest customer messaging: "we support BigQuery today; Snowflake is 3 weeks away once we sign our first Snowflake customer."

## Single-customer override

If a single Snowflake customer commits to paying before launch, swap order — ship Snowflake first. The interface guarantees it's the same lift either way.

## Phase 2 connector additions (in priority order)

1. Snowflake — full integration tests, dbt-snowflake compat
2. Databricks — Delta-log CDC, Unity Catalog auth
3. Redshift — connector + tests
4. Synapse — only if a customer asks
5. ClickHouse — only if a customer asks
