# `internal/protocols/pgwire/` — Postgres Wire Protocol v3

Canonical: `docs/architecture/pg-wire-protocol.md`. Required gate: `pg-protocol-validator` agent before any merge that touches this package.

**Listen port:** `UV_PROXY_PORT` — `5432` in prod, `5000` in dev/test (avoids colliding with a local Postgres). Tests connect to `localhost:5000`.

## Invariants

- **Parse the database field as `{customer_id}_{warehouse}`** (e.g. `acme_bigquery`). User field = API key.
- **Support Simple + Extended query.** Parse / Bind / Execute / Describe / Sync / Terminate.
- **Map warehouse errors → Postgres SQLSTATE codes** per `docs/conventions/error-mapping.md`.
- **TLS:** self-signed cert in dev (auto-generated on first start), customer cert in prod via `TLS_CERT_PATH` / `TLS_KEY_PATH`.
- **Authentication:** md5 (Phase 1) + cleartext-over-TLS (Phase 1) + SCRAM-SHA-256 (Phase 2).

## Files

`server.go` (listener) · `protocol.go` (parse/serialize) · `session.go` (per-conn state) · `auth.go` (API-key validation against control plane).

Never add business logic here — pass extracted SQL + auth.Principal to `internal/router`.
