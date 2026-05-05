# `internal/workers/` — DuckDB Worker Pool

Canonical: `docs/architecture/duckdb-pool.md`.

## Invariants

- **Pre-warm on customer connect.** Don't lazy-init on first query.
- **Per-customer namespacing.** Tables in DuckDB are `{customer_id}_{schema}_{table}`. Never collide.
- **CGo bindings via `marcboeker/go-duckdb`.** Document M-series build flags in `docs/reference/requirements.md`.
- **`CHECKPOINT` + re-attach on Iceberg snapshot refresh** (Redis pubsub event from `internal/sync`).
- **Hard timeout 30s** via `context.WithTimeout`. Cancellation must abort the DuckDB query (not just the goroutine).
- **Never on proxy main goroutine.** Always run inside a worker goroutine with bounded pool size (default 3 per customer).

## Files

`pool.go` · `worker.go` · `attach.go` (Iceberg ATTACH syntax for S3 + GCS) · `executor.go` · `refresh.go`.
