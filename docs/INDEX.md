# Ultraviolet Documentation Index

Authoritative list of every doc in `docs/`. Organized by **consumption topic** (what task needs them), not document type. Update when adding or renaming docs (per `docs/CLAUDE.md`).

**Read `docs/CLAUDE.md` first** for doc discipline (read-first, write-first, conflict precedence).

---

## `strategy/` — Business + market context (rare reads)

| Doc | Purpose |
|---|---|
| [`pricing.md`](strategy/pricing.md) | 70–90% savings model; cost-of-goods; pricing tiers (TBD) |
| [`competitive-landscape.md`](strategy/competitive-landscape.md) | vs Greybeam / Keebo / Espresso / MotherDuck / Materialize — feature matrix + Ultraviolet's wedge |

## `architecture/` — How the system is built (frequent reads)

| Doc | Purpose |
|---|---|
| [`overview.md`](architecture/overview.md) | High-level diagram + component responsibilities |
| [`pg-wire-protocol.md`](architecture/pg-wire-protocol.md) | Protocol-level invariants for `internal/protocols/pgwire/` |
| [`client-protocols.md`](architecture/client-protocols.md) | PG-wire / ADBC / Snowflake-wire matrix; which BI tool uses which |
| [`routing.md`](architecture/routing.md) | DuckDB-vs-warehouse decision tree + hybrid pushdown (Phase 2) |
| [`duckdb-pool.md`](architecture/duckdb-pool.md) | Worker lifecycle, Iceberg attach, refresh on snapshot pubsub |
| [`cdc-sync.md`](architecture/cdc-sync.md) | Snowflake STREAM, BigQuery APPENDS / watermark, Databricks Delta-log |
| [`iceberg.md`](architecture/iceberg.md) | Writer strategy (DuckDB-Iceberg ext default + Go custom escape) |
| [`iceberg-modes.md`](architecture/iceberg-modes.md) | Sync / catalog passthrough / BYO Iceberg |
| [`ai-rewriter.md`](architecture/ai-rewriter.md) | `ai_generate()` paths A/B + model-name mapping |
| [`multi-warehouse.md`](architecture/multi-warehouse.md) | Connector interface; "build for 3, launch with 1" |
| [`storage-modes.md`](architecture/storage-modes.md) | Managed S3 vs BYOS (customer S3/GCS) |
| [`observability.md`](architecture/observability.md) | Query log schema, metrics, traces |
| [`cost-attribution.md`](architecture/cost-attribution.md) | Per-warehouse cost APIs; nightly backfill |
| [`warehouse-auth.md`](architecture/warehouse-auth.md) | Auth modes per warehouse (Snowflake JWT-PKCS8, BQ WIF, Databricks PAT) |
| [`deployment-models.md`](architecture/deployment-models.md) | SaaS / BYOC / self-hosted |

## `process/` — How we work (frequent reads)

| Doc | Purpose |
|---|---|
| [`ultrathink.md`](process/ultrathink.md) | **Mandatory first stop** for every non-trivial plan (11-axis edge case checklist) |
| [`plan-before-code.md`](process/plan-before-code.md) | The meta-rule + when to invoke swarm-coordinator |
| [`no-fallback-data.md`](process/no-fallback-data.md) | Hard rule + enforcement examples (DuckDB→warehouse fallback discipline) |
| [`file-size-limits.md`](process/file-size-limits.md) | ≤1000-line cap + splitting strategy |
| [`compile-cleanly.md`](process/compile-cleanly.md) | Per-language verification commands; the import-error trap |
| [`code-cleanliness.md`](process/code-cleanliness.md) | Search-before-write, duplication taxonomy, security hygiene |
| [`testing.md`](process/testing.md) | Testing pyramid; BQ-public-data integration tests; BI-tool golden suites |
| [`agents-and-swarm.md`](process/agents-and-swarm.md) | Trigger thresholds, gate flow, worktree cleanup |
| [`claude-code-best-practices.md`](process/claude-code-best-practices.md) | 2026 synthesis — Anthropic + community |

## `conventions/` — Naming + style (occasional reads)

| Doc | Purpose |
|---|---|
| [`go-style.md`](conventions/go-style.md) | Error wrapping, context propagation, no naked goroutines |
| [`logging.md`](conventions/logging.md) | zerolog format, normalized-SQL-only in prod, PII rules |
| [`git-workflow.md`](conventions/git-workflow.md) | Branch naming, conventional commits, PR rules |
| [`error-mapping.md`](conventions/error-mapping.md) | Warehouse error → Postgres SQLSTATE table |
| [`naming.md`](conventions/naming.md) | Customer namespacing, env var prefixes |

## `reference/` — Spec + requirements (rare reads)

| Doc | Purpose |
|---|---|
| [`product-brief.md`](reference/product-brief.md) | Full original product brief (architecture, phases, env vars) |
| [`phase-1-build-order.md`](reference/phase-1-build-order.md) | The 13-step ordered build plan |
| [`env-vars.md`](reference/env-vars.md) | Catalogue of every env var + meaning |
| [`requirements.md`](reference/requirements.md) | System requirements (Go 1.22+, CGo, Docker, LocalStack) |

## `changelog/` — Change tracking

| Doc | Purpose |
|---|---|
| [`CHANGELOG.md`](changelog/CHANGELOG.md) | Architectural decision log (reverse chronological) |
| [`IMPROVEMENTS.md`](changelog/IMPROVEMENTS.md) | Open observations / deferred work |
