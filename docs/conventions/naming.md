# Naming Conventions

## Customer namespacing

Format: `{customer_id}_{schema}_{table_name}`. Customer IDs are short slugs (`acme`, `glob_corp`) — generated from company-name input + uniqueness check during onboarding. Lowercase, alphanumeric + underscore.

Examples:
- `acme_public_users`
- `acme_analytics_events`
- `glob_corp_dimcustomer`

Used in:
- DuckDB ATTACH names (`internal/workers/attach.go`)
- Iceberg table location prefixes (`s3://uv-data/{customer_id}/{table_name}/`)
- Redis pubsub channels (`customer:{id}:table:{name}:refreshed:{snap}`)

Never collisions between customers: a global `code-organizer` rule rejects any code path that constructs a DuckDB table name without the customer ID prefix.

## Connection-string database field

Format: `{customer_id}_{warehouse_type}` — e.g., `acme_bigquery`, `acme_snowflake`. Parsed by `internal/protocols/pgwire/server.go` from the PG StartupMessage `database` field.

`warehouse_type` is one of: `bigquery` · `snowflake` · `databricks` · `redshift` (Phase 2+).

A customer can have multiple connections — `acme_bigquery` and `acme_snowflake` are two distinct routes for the same customer. The `connections` table maps `(customer_id, warehouse_type)` → connection ID.

## API keys

Format: `uv_<random32>`. Generated as `crypto/rand` 32 bytes → base62 encoded. Prefix `uv_` makes leak-detection regex easy: `^uv_[A-Za-z0-9]{32}$`.

Stored hashed (Argon2id) in `api_keys.key_hash`; never stored plaintext.

Display: shown ONCE at creation in the UI; user copies. After that only last-4 visible (`uv_***Wq8z`).

## Env var prefixes

All Ultraviolet env vars: `UV_*` or warehouse-standard names where third-party SDKs expect them.

| Prefix | Use |
|---|---|
| `UV_*` | Ultraviolet config (`UV_LOG_LEVEL`, `UV_PROXY_PORT`, `UV_DEV_TLS`, ...) |
| `AWS_*` | AWS SDK standard (passed through) |
| `GOOGLE_APPLICATION_CREDENTIALS` | Google SDK standard |
| `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` | LLM SDK standard |
| `DATABASE_URL` / `REDIS_URL` | de-facto standard for those services |
| `JWT_SECRET` / `ENCRYPTION_KEY` | unprefixed for compatibility with secret-manager auto-mount |

Full catalogue in `docs/reference/env-vars.md`.

## Postgres schema (control plane)

- Tables: snake_case singular preferred (`customer`, not `customers`)? — actually plural per common Go convention (`customers`, `connections`). Pick plural; consistent across the schema.
- Columns: snake_case (`customer_id`, `created_at`).
- Timestamps: `created_at` + `updated_at` (UTC, `TIMESTAMPTZ`).
- Soft delete: `deleted_at` (NULL when not deleted).
- IDs: UUID v7 preferred (sortable); column type `UUID`.
- FK names: `<table>_<other_table>_fkey` (golang-migrate default).
- Index names: `<table>_<columns>_idx`.

## Go packages

Lowercase, single word where possible. Multi-word: omit underscores (`pgwire`, not `pg_wire`). Match folder name. Acronyms keep all-lower (`api`, not `API`).

## Test users + credentials

Test/dev customer IDs use `test_*` prefix (`test_acme`). Production never uses `test_*`.

For BQ public-data integration tests: customer `test_uv` with connection name `test_uv_bq_public`.

For load tests: `test_load_001`, `test_load_002`, ... with reserved DuckDB pool capacity.

## File naming

Per `docs/conventions/go-style.md` — primary file matches package main concept; helper files name the secondary concern.

`migrations/` — `NNNN_description.up.sql` / `NNNN_description.down.sql`. Zero-padded NNNN, monotonic, no gaps.
