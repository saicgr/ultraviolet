# Client Protocol Matrix

Three protocols, three phases.

| Protocol | Phase | Package | Default port | Auth | Why |
|---|---|---|---|---|---|
| Postgres wire v3 | 1 | `internal/protocols/pgwire/` | 5432 (prod) / 5000 (dev) | API key in `user` startup field; md5 / cleartext-over-TLS | Universal — every BI tool, every SQL client supports it |
| ADBC (Arrow Flight SQL) | 1.5 | `internal/protocols/adbc/` | 50051 (gRPC) | Bearer token | Arrow throughput; preferred by dbt, Tableau (newer), Hex |
| Snowflake wire | 2 | `internal/protocols/snowflake-wire/` | 8001 (HTTPS REST) | JWT-PKCS8 / OAuth | Drop-in for existing Snowflake driver users |

## Which BI tool uses which

| Tool | Phase 1 (PG-wire) | Phase 1.5 (ADBC) | Phase 2 (SF-wire) |
|---|---|---|---|
| psql | ✅ native | — | — |
| dbt-postgres | ✅ native | — | — |
| dbt-snowflake | — | — | ✅ |
| Looker | ✅ via PG dialect | — | ✅ via SF dialect |
| Tableau | ✅ via PG ODBC | ✅ ADBC connector | ✅ |
| Hex | ✅ | ✅ preferred | — |
| Mode | ✅ | — | — |
| DBeaver / TablePlus | ✅ | — | — |
| Python `psycopg2` / `psycopg3` | ✅ | — | — |
| Python `snowflake-connector-python` | — | — | ✅ |

## Cross-protocol invariants (`internal/protocols/CLAUDE.md`)

1. All three protocols hand a normalized `(SQL, auth.Principal, session)` tuple to `internal/router`. No protocol-specific routing logic.
2. Per-customer table namespacing applied at protocol boundary, before SQL hits router.
3. Connection-level auth converted to a uniform `auth.Principal{CustomerID, WarehouseType, APIKeyID}`.
4. Errors translated to protocol-correct frames before write — PG ErrorResponse, ADBC error metadata, SF JSON error envelope.

## Phase ordering rationale

PG-wire first because (a) every tool already speaks it, (b) the brief specs it, (c) BigQuery has no native client driver other than ADBC + REST — PG-wire IS our client surface.

ADBC at 1.5 once we have customers complaining about throughput on large extracts (Tableau extracts, dbt incremental scans). Building it requires Arrow Flight SQL server in Go (`apache/arrow-go`).

Snowflake-wire at Phase 2 because customers with deep Snowflake-driver investment (Snowpark, snowsql, internal tooling) won't switch their connection string to PG. Lower priority than getting BigQuery + Snowflake-via-PG working first.
