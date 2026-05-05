# Error Mapping — Warehouse → Postgres SQLSTATE

Returned in PG-wire ErrorResponse `C` field. Implemented in `internal/connectors/errors.go` per warehouse.

## General principles

- **Preserve message, translate code.** Original error message kept (after PII scrub); SQLSTATE matches the closest PG semantic.
- **Specific over generic.** Prefer a specific SQLSTATE (`42501` for permission) over `XX000` (internal).
- **No new SQLSTATE families** — stick to standard PG codes so BI tools' error handlers work.

## Snowflake → SQLSTATE

| Snowflake code | SQLSTATE | Meaning |
|---|---|---|
| 1003 (syntax) | `42601` | syntax_error |
| 2003 (auth) | `28000` | invalid_authorization |
| 2043 (user not exist) | `28000` | invalid_authorization |
| 90030 / 90031 (role) | `42501` | insufficient_privilege |
| 100099 (compilation) | `42P01` (table) / `42703` (column) | depends on cause |
| 390114 (token expired) | `28000` | invalid_authorization (auto-retry) |
| 604 (cancelled) | `57014` | query_canceled |
| 100025 / 100027 (timeout) | `57014` | query_canceled |
| network / 5xx | `08006` | connection_failure |
| any other | `XX000` | internal_error (preserve `errno=N` in detail) |

## BigQuery → SQLSTATE

| BQ HTTP / reason | SQLSTATE | Meaning |
|---|---|---|
| 400 invalidQuery | `42601` | syntax_error |
| 403 accessDenied | `42501` | insufficient_privilege |
| 404 notFound (table) | `42P01` | undefined_table |
| 404 notFound (dataset) | `3F000` | invalid_schema_name |
| 401 unauthenticated | `28000` | invalid_authorization |
| 409 duplicate | `42710` | duplicate_object |
| 409 jobAlreadyExists | `42710` | duplicate_object |
| 429 rateLimitExceeded | `53400` | configuration_limit_exceeded |
| 500 / 503 | `08006` | connection_failure (retry) |
| `cancelled` | `57014` | query_canceled |
| any other | `XX000` | internal_error |

## Databricks → SQLSTATE (Phase 2)

| Databricks error | SQLSTATE |
|---|---|
| `PARSE_SYNTAX_ERROR` | `42601` |
| `PERMISSION_DENIED` | `42501` |
| `TABLE_OR_VIEW_NOT_FOUND` | `42P01` |
| `OPERATION_CANCELED` | `57014` |
| timeout | `57014` |
| 4xx auth | `28000` |
| 5xx | `08006` |

## DuckDB internal → SQLSTATE

| DuckDB error | SQLSTATE | Meaning |
|---|---|---|
| Parser error | `42601` | syntax_error |
| Catalog error | `42P01` / `42703` | undefined table/column |
| Conversion error | `22023` | invalid_parameter_value |
| Out of memory | `53200` | out_of_memory |
| IO error (S3) | `08006` | connection_failure |
| Internal error | `XX000` | internal_error |

## Common SQLSTATE used by Ultraviolet

| Code | Class | Use |
|---|---|---|
| `08006` | connection_failure | warehouse/network unreachable |
| `0A000` | feature_not_supported | LISTEN/NOTIFY, replication, 2PC |
| `22023` | invalid_parameter_value | bad ai_generate model name |
| `28000` | invalid_authorization | bad API key, bad warehouse creds |
| `3F000` | invalid_schema_name | unknown dataset / database |
| `42501` | insufficient_privilege | warehouse permission denied |
| `42601` | syntax_error | parser failure (PG or warehouse) |
| `42703` | undefined_column | column not in table |
| `42P01` | undefined_table | table not found / not synced |
| `42710` | duplicate_object | duplicate create |
| `53200` | out_of_memory | DuckDB / proxy OOM |
| `53300` | too_many_connections | pool exhausted |
| `53400` | configuration_limit_exceeded | rate limit (LLM, warehouse) |
| `57014` | query_canceled | context cancel / timeout |
| `XX000` | internal_error | unexpected proxy bug |

## ErrorResponse field discipline

- `S` Severity: `ERROR` (default), `FATAL` (terminate connection), `PANIC` (server going down).
- `V` Severity (V3): same as S.
- `C` SQLSTATE: from table above.
- `M` Message: short, human-readable, no credentials.
- `D` Detail: original warehouse error code + scrubbed message.
- `H` Hint: action user can take ("check that table is synced", "rotate API key").
- `R` Routine: `proxy_route_query` / `worker_execute` / `connectors_<warehouse>_execute`.

Example:
```
S=ERROR V=ERROR C=42P01 M="relation acme.orders does not exist"
D="BigQuery 404 notFound: Not found: Table proj:acme.orders"
H="Add the table to UV sync at /sync/tables, or verify the table name is correct in the warehouse."
R=worker_execute
```
