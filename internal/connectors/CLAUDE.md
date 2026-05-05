# `internal/connectors/` — Warehouse Connectors

Canonical: `docs/architecture/multi-warehouse.md`. Required gate: `warehouse-connector-tester` agent.

## Connector interface (all warehouses implement)

```go
type Connector interface {
    Execute(ctx context.Context, sql string) (ResultSet, error)
    Cancel(ctx context.Context, queryID string) error
    Introspect(ctx context.Context) ([]Table, error)
    Close() error
}
```

## Phase 1 wiring

- **BigQuery** (`bigquery.go`) — fully wired. SDK: `cloud.google.com/go/bigquery`. Auth modes per `docs/architecture/warehouse-auth.md` (service-account JSON / OAuth2 / WIF).
- **Snowflake** (`snowflake.go`) — connector struct + interface conformance, but Phase 1 only forwards. SDK: `snowflakedb/gosnowflake`. Auth: password / JWT-PKCS8 / OAuth.
- **Databricks** (`databricks.go`) — stub. Phase 2 wiring.

## Discipline

- **Connection pool per customer**, max 5. Never share connections across customers.
- **Type mapping → `result.go` → Postgres OIDs** — see `docs/conventions/error-mapping.md` + `internal/protocols/pgwire/types.go`.
- **Stream large results.** BQ Storage Read API for >100k rows; Snowflake Arrow batches for chunked reads.
- **Always-on integration tests against `bigquery-public-data.*`.** No mocks.
