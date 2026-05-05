# Ultraviolet

> Multi-warehouse query proxy. Postgres-wire on the front (Phase 1), DuckDB + Iceberg on the back. Routes cheap reads off Snowflake / BigQuery / Databricks; provides warehouse-agnostic `ai_generate()`. Target: 70–90% warehouse cost cut, zero BI-tool changes.

## Quick start (dev)

```bash
# 1. Toolchain (macOS): brew install go node pnpm docker make golang-migrate sqlc golangci-lint shellcheck
# 2. Dev services
make dev                # starts Postgres + Redis + LocalStack via docker-compose
# 3. Build (Phase 1 binaries land here)
make build
# 4. Verify
make verify             # lint + tests
# 5. Connect a client (proxy listens on :5000 in dev — avoids the local Postgres on :5432)
psql -h localhost -p 5000 -U <api_key> -d <customer>_<warehouse>
```

## Repo layout

```
/CLAUDE.md              entry-point routing for Claude Code
/.claude/               agents (12), skills (5), commands (1), hooks
/cmd/{proxy,api,sync}   binaries
/internal/              business logic (protocols, router, workers, connectors, sync, iceberg, ai, api, store, cost)
/frontend/              React + Tailwind + shadcn control-plane UI
/docs/                  topical docs — see docs/INDEX.md
/migrations/            golang-migrate SQL files
/test/integration/      tests against bigquery-public-data.* and LocalStack
/scripts/               swarm + ops helpers
```

## Documentation

- **Start at:** [`docs/INDEX.md`](docs/INDEX.md)
- **Phase 1 build order:** [`docs/reference/phase-1-build-order.md`](docs/reference/phase-1-build-order.md)
- **Architecture:** [`docs/architecture/overview.md`](docs/architecture/overview.md)
- **Full product brief:** [`docs/reference/product-brief.md`](docs/reference/product-brief.md)

## Status

Phase 0 (scaffold) complete. Phase 1 (MVP) not started.

## License

TBD.
