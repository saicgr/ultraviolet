# Logging Format + PII Rules

Library: [`zerolog`](https://github.com/rs/zerolog). Format: JSON in prod, pretty-console in dev (`UV_LOG_FORMAT=pretty`).

## Format

```json
{
  "level": "info",
  "time": "2026-05-05T12:34:56.789Z",
  "service": "uv-proxy",
  "trace_id": "abc123...",
  "span_id": "def456...",
  "customer": "acme",
  "request_id": "...",
  "msg": "query.completed",
  "route": "duckdb",
  "duration_ms": 42,
  "rows": 150,
  "sql_hash": "7af0..."
}
```

Top-level fields always present: `level`, `time`, `service`, `msg`. Trace + span IDs propagated from OpenTelemetry context. Request ID per HTTP/PG-conn invocation.

## Levels

| Level | Use |
|---|---|
| `trace` | dev only; verbose protocol byte dumps |
| `debug` | dev + staging; component decisions; per-step state |
| `info` | normal operation events (`query.completed`, `connection.opened`) |
| `warn` | recoverable errors, fallbacks, retries |
| `error` | non-recoverable per-request errors that didn't crash the process |
| `fatal` | process crashes |

`UV_LOG_LEVEL` controls minimum; default `info` in prod, `debug` in dev.

## Event naming

`subject.verb` snake-case. Examples:
- `proxy.connection.opened`
- `proxy.connection.closed`
- `query.completed`
- `query.failed`
- `router.decision`
- `router.fallback.warehouse`
- `worker.checkout`
- `worker.timeout`
- `sync.snapshot.committed`
- `sync.backpressure`
- `iceberg.commit.failed`
- `ai.batch.submitted`
- `cost.backfill.completed`

## PII rules (HARD)

| Field | In dev | In staging | In prod |
|---|---|---|---|
| Raw SQL with literals | `debug` | NEVER | NEVER |
| Normalized SQL (literals stripped) | `debug` | `info` | `info` |
| `sql_hash` (sha256 of normalized) | `info` | `info` | `info` |
| API key (full) | NEVER | NEVER | NEVER |
| API key (last 4) | `debug` | `debug` | `debug` |
| Customer ID | always | always | always (it's an opaque UUID) |
| Customer name (e.g., "acme") | `info` | `info` | `info` |
| Warehouse credentials (any form) | NEVER | NEVER | NEVER |
| Warehouse query ID | always | always | always (used for cost lookup) |
| User email | `debug` (test users only) | `debug` | NEVER |

The `internal/logger` package wraps zerolog with a `LogQuery(ctx, sql, ...)` helper that normalizes + hashes SQL before any output. The raw SQL never leaves `internal/store/query_log_writer.go` (which encrypts it before insert).

## Stack traces

- `error` log → include sanitized stack trace (top 10 frames; package paths, not full file paths in prod).
- `warn` log → no stack trace.
- Errors returned to clients (PG ErrorResponse) → never include stack trace.

## Rate limiting / sampling

For high-frequency events (per-row trace), use sampling:
```go
if log.Sample(zerolog.OftenSampler).Enabled() {
    log.Sample(zerolog.OftenSampler).Trace()...
}
```

Don't sample errors or warns.

## Anti-patterns

- ❌ String interpolation: `log.Printf("query failed: %s", err)` → use `log.Error().Err(err).Msg("query.failed")`.
- ❌ Logging raw SQL in `info`: leaks PII.
- ❌ Per-row debug logs: floods at scale; sample.
- ❌ Logging credentials at any level: hard ban.
- ❌ Custom timestamp formats: use `time` field zerolog default.
