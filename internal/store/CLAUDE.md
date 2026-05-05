# `internal/store/` — Persistence Layer

Stack: control-plane Postgres 16 + golang-migrate + sqlc.

## Discipline

- **All schema changes via `migrations/` files** — `golang-migrate` numbered up/down.
- **All queries via sqlc** — write SQL in `internal/store/queries/*.sql`, run `make sqlc`, use generated funcs. Never hand-write SQL strings.
- **Never edit applied migrations.** Add a new migration; the `code-organizer` agent rejects edits to applied files.
- **Schema tables:** `customers`, `connections`, `synced_tables`, `query_log`, `api_keys`, `sync_jobs`, `cost_attribution`.
- **Encrypted columns** (warehouse credentials, LLM API keys) use AES-256-GCM via `internal/store/crypto.go`. Key sourced from `ENCRYPTION_KEY` env.

## Files

`db.go` (connection pool) · `crypto.go` · `queries/*.sql` · `gen/*.go` (sqlc output, gitignored? — kept committed for build determinism, like Reppora's `.g.dart`).
