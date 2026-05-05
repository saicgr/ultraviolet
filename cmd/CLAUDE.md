# `cmd/` — Binary Entry Points

Three binaries: `proxy/` (PG-wire server, `:5432` prod / `:5000` dev), `api/` (REST control plane, `:8080`), `sync/` (CDC workers).

## Discipline

- **Wiring only.** No business logic in `main.go`. Parse config, build dependencies, call `internal/<pkg>` constructors, start the server.
- **Graceful shutdown.** Listen for SIGTERM/SIGINT; cancel root context; wait on connection drain (max 30s).
- **One config source.** Load via `internal/config`. Never read env vars directly inside cmd files.

## Files

- `cmd/proxy/main.go` — PG-wire listener, accepts connections, hands off to `internal/protocols/pgwire`.
- `cmd/api/main.go` — chi router, mounts `internal/api` handlers.
- `cmd/sync/main.go` — scheduler that runs per-customer CDC pollers from `internal/sync`.

## When adding a new binary

Add a new subdir + entry in `Makefile` (`build-<name>` target) + `docker-compose.yml` service. Update `docs/reference/product-brief.md` §Directory Structure.
