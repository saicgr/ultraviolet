# `internal/api/` — REST Control Plane

Endpoints listed in `docs/reference/product-brief.md` §8. Stack: chi router + zerolog + sqlc.

## Middleware order

1. Recover panic → 500 + structured log
2. Request ID + trace ID
3. CORS (control-plane UI origin only)
4. Auth — split:
   - JWT (UI sessions): `/api/v1/auth/*` issues, others verify
   - API key (programmatic): `X-API-Key` header validated against `api_keys` table
5. Rate limit (per principal)
6. Handler

## Discipline

- **No SQL in handlers.** Use sqlc-generated query funcs from `internal/store`.
- **No business logic in handlers.** Handler = parse → call service → render.
- **Versioned URLs.** `/api/v1/...` only; bump prefix for breaking changes.
- **OpenAPI spec auto-generated** in CI (Phase 1.5).

## Files

`router.go` · `middleware.go` · `auth.go` · `connections.go` · `sync.go` · `queries.go` · `analytics.go` · `apikeys.go`.
