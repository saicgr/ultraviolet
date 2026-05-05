# Postgres Wire Protocol (v3) — Ultraviolet Invariants

Reference: skill `pg-wire-reference` for message catalogue + OID table. This doc is Ultraviolet-specific layered semantics.

## Connection lifecycle

1. TCP accept on `UV_PROXY_PORT` (default `5432` in prod, `5000` in dev/test to avoid colliding with a local Postgres).
2. Read StartupMessage. Parse:
   - `database` field = `{customer_id}_{warehouse}` (e.g., `acme_bigquery`).
   - `user` field = API key (32-char `uv_...` token).
3. AuthenticationRequest (md5 challenge) → PasswordMessage round trip. **Cleartext only over TLS.**
4. Validate API key against control-plane DB (`internal/store.GetAPIKey`). If valid, look up `customer_id`, `warehouse_type`, encrypted creds.
5. Decrypt warehouse creds (AES-256-GCM, key from `ENCRYPTION_KEY`).
6. Spawn (or reuse) DuckDB workers for this customer (`internal/workers.Pool.Checkout`).
7. ReadyForQuery 'I' → wait for queries.

## Per-query loop (Simple Query)

1. Receive `Q` SimpleQuery.
2. Parse SQL via `pg_query_go` (real PG parser, no regex).
3. Hand to `internal/router.Route(ctx, sql, principal)`.
4. Router returns `Decision{Target: "duckdb"|"warehouse"|"hybrid", RewrittenSQL: ...}`.
5. Execute on chosen target; stream rows back as RowDescription + DataRow + CommandComplete.
6. ReadyForQuery 'I'.

## Per-query loop (Extended Query)

Sequence: Parse → Bind → Describe (optional) → Execute → Sync. Each statement gets a server-side prepared name. Multiple bind/execute against the same parse allowed.

Discipline:
- Always echo `1` ParseComplete, `2` BindComplete, `3` CloseComplete in order.
- `D` Describe with 'S' returns ParameterDescription + RowDescription. `D` with 'P' returns RowDescription only.
- `Sync` flushes pending state; emit ReadyForQuery.

## Error handling

- Catch any panic anywhere in the per-connection goroutine; emit ErrorResponse with `XX000` SQLSTATE; close connection.
- Map warehouse errors per `docs/conventions/error-mapping.md`.
- Never include credentials, internal stack traces, or other customer SQL in the error message returned to the client.

## TLS

- Self-signed cert auto-generated in dev (`UV_DEV_TLS=true`).
- Customer certs in prod via `TLS_CERT_PATH` / `TLS_KEY_PATH`.
- `PROXY_REQUIRE_TLS=true` in prod — reject any connection that doesn't request SSL via the SSLRequest preamble.
- TLS 1.2+ only; cipher allowlist in code.

## Cancellation

- BackendKeyData sent at session start (random `(processID, secretKey)`).
- CancelRequest is a separate connection — match by `(processID, secretKey)`, look up the running query's warehouse query ID, call `connector.Cancel(queryID)` AND cancel the goroutine context.

## Session state

Per-connection state held in `internal/protocols/pgwire/session.go`:
- Current schema (`SET search_path` ...)
- Open transactions (Phase 1: forward `BEGIN`/`COMMIT` to warehouse; DuckDB-routed queries are auto-commit only)
- Prepared statements
- Bound portals
- ParameterStatus echoes (`server_version`, `client_encoding`, `DateStyle`, ...)

## Things deliberately NOT supported (Phase 1)

- LISTEN / NOTIFY → ErrorResponse `0A000` feature_not_supported.
- COPY FROM STDIN / TO STDOUT → forwarded raw to warehouse if it supports it.
- Two-phase commit (`PREPARE TRANSACTION`) → `0A000`.
- Replication protocol → `0A000`.
