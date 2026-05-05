# `internal/protocols/` — Client-Facing Protocol Surface

Three sub-packages, one per protocol. See `docs/architecture/client-protocols.md` for the matrix.

| Package | Phase | Used by |
|---|---|---|
| `pgwire/` | 1 | psql, dbt-postgres adapter, generic SQL clients |
| `adbc/` | 1.5 | Arrow Flight SQL — performant BI (Tableau, Hex over ADBC) |
| `snowflake-wire/` | 2 | Snowflake driver compatibility — drop-in for existing Snowflake clients |

## Cross-protocol invariants

- **Same router downstream.** All three protocols hand the parsed SQL + session to `internal/router` — no per-protocol routing logic.
- **Auth extracted at protocol boundary.** Convert protocol-specific auth (PG startup msg user field, ADBC bearer, Snowflake JWT) into a uniform `auth.Principal` before routing.
- **Per-customer namespacing applied at protocol boundary.** `{customer_id}_{schema}_{table}` rewriting happens before SQL hits the router.
- **Never crash on malformed input.** Catch panics; return protocol-correct error frames.

See `docs/architecture/pg-wire-protocol.md` + `internal/protocols/pgwire/CLAUDE.md` for PG-specific rules.
