---
name: pg-wire-reference
description: Quick reference for Postgres wire protocol v3 message types, encoding rules, and OID-to-type mappings. Use when implementing or debugging any code in internal/protocols/pgwire/, when the pg-protocol-validator agent reports a malformed message, or when mapping warehouse types back to Postgres OIDs in result.go.
---

# pg-wire-reference skill

## Message frame format

Every backend/frontend message except StartupMessage:
```
| Type (1 byte) | Length (4 bytes BE, includes itself) | Payload |
```

StartupMessage has no type byte; just `Length || Payload`.

## Frontend → Backend (client → us)

| Type | Name | Notes |
|---|---|---|
| (none) | StartupMessage | First msg; protocol version 3.0 = `0x00030000` |
| `Q` | SimpleQuery | Single SQL string, null-terminated |
| `P` | Parse | name, query, paramOIDs[] |
| `B` | Bind | portal, stmt, formatCodes[], params[], resultFormatCodes[] |
| `E` | Execute | portal, maxRows |
| `D` | Describe | 'S' (statement) or 'P' (portal), name |
| `S` | Sync | end of extended-query batch |
| `H` | Flush | force buffered output |
| `C` | Close | 'S' or 'P', name |
| `X` | Terminate | clean disconnect |
| `p` | PasswordMessage | for AuthRequest response |

## Backend → Frontend (us → client)

| Type | Name |
|---|---|
| `R` | AuthenticationRequest (subtype: 0=OK, 5=md5, 10=SASL, 11=SASLContinue, 12=SASLFinal) |
| `S` | ParameterStatus (e.g., `server_version`, `client_encoding`) |
| `K` | BackendKeyData (for cancel) |
| `Z` | ReadyForQuery ('I'=idle, 'T'=in xact, 'E'=failed xact) |
| `T` | RowDescription |
| `D` | DataRow |
| `C` | CommandComplete (e.g., `SELECT 42`) |
| `1` | ParseComplete |
| `2` | BindComplete |
| `3` | CloseComplete |
| `n` | NoData |
| `t` | ParameterDescription |
| `E` | ErrorResponse (fields: S, V, C, M, D, H, P, p, q, W, s, t, c, d, n, F, L, R) |
| `N` | NoticeResponse |

## RowDescription field

```
int16 nFields
[per field]
  string name
  int32 tableOID (0 if unknown)
  int16 columnAttrNum (0 if unknown)
  int32 typeOID
  int16 typeSize (-1 if variable)
  int32 typmod (-1 if no modifier)
  int16 formatCode (0=text, 1=binary)
```

## OID quick reference (most common)

| OID | Type | Postgres name | BQ map | Snowflake map |
|---|---|---|---|---|
| 16 | bool | BOOL | BOOL | BOOLEAN |
| 20 | int8 | BIGINT | INT64 | NUMBER (precision ≤19, scale 0) |
| 21 | int2 | SMALLINT | — | — |
| 23 | int4 | INTEGER | — | — |
| 700 | float4 | REAL | — | FLOAT |
| 701 | float8 | DOUBLE PRECISION | FLOAT64 | FLOAT |
| 1043 | varchar | VARCHAR | STRING | VARCHAR |
| 25 | text | TEXT | STRING (no length) | TEXT |
| 1082 | date | DATE | DATE | DATE |
| 1114 | timestamp | TIMESTAMP (no tz) | DATETIME | TIMESTAMP_NTZ |
| 1184 | timestamptz | TIMESTAMP WITH TIME ZONE | TIMESTAMP | TIMESTAMP_LTZ |
| 1700 | numeric | NUMERIC | NUMERIC, BIGNUMERIC | NUMBER |
| 17 | bytea | BYTEA | BYTES | BINARY |
| 114 | json | JSON | JSON | VARIANT (json subset) |
| 3802 | jsonb | JSONB | JSON | VARIANT |

## ErrorResponse SQLSTATE codes (Ultraviolet-relevant)

- `08006` connection_failure → warehouse unreachable
- `28000` invalid_authorization_specification → bad API key
- `42501` insufficient_privilege → warehouse permission denied
- `42P01` undefined_table → table not synced AND not present in warehouse
- `53300` too_many_connections → DuckDB pool exhausted
- `57014` query_canceled → context deadline / explicit cancel
- `XX000` internal_error → unexpected proxy bug

## See also

- [PostgreSQL frontend/backend protocol docs](https://www.postgresql.org/docs/16/protocol.html)
- `docs/architecture/pg-wire-protocol.md` — Ultraviolet-specific invariants
