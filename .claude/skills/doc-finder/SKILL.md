---
name: doc-finder
description: Topic-indexed lookup over Ultraviolet's docs/ tree. Maps task keywords to the right subfolder + file. Faster than `find docs/` + filename inspection. Invoke when planning a task to locate the canonical doc.
---

# doc-finder skill

## Routing table

### Strategy / pricing / competitors
→ `docs/strategy/`
- Pricing math, savings model → `pricing.md`
- Competitive positioning, vs Greybeam/Keebo/Espresso/MotherDuck → `competitive-landscape.md`

### Architecture / system design
→ `docs/architecture/`
- High-level diagram → `overview.md`
- PG wire protocol invariants → `pg-wire-protocol.md`
- Client protocols (PG / ADBC / Snowflake-wire) → `client-protocols.md`
- Routing decision tree → `routing.md`
- DuckDB pool, attach lifecycle → `duckdb-pool.md`
- CDC sync (Snowflake STREAM, BQ APPENDS, Databricks Delta) → `cdc-sync.md`
- Iceberg writer / spec → `iceberg.md`
- Iceberg modes (sync / passthrough / BYO) → `iceberg-modes.md`
- AI rewriter paths A/B → `ai-rewriter.md`
- Multi-warehouse interface → `multi-warehouse.md`
- Storage modes (managed S3 vs BYOS) → `storage-modes.md`
- Observability (query log, metrics) → `observability.md`
- Cost attribution per warehouse → `cost-attribution.md`
- Warehouse auth modes → `warehouse-auth.md`
- Deployment models (SaaS / BYOC / self-hosted) → `deployment-models.md`

### Process / workflow
→ `docs/process/`
- Planning a non-trivial change → `ultrathink.md` (mandatory first stop)
- The plan-before-code rule → `plan-before-code.md`
- The no-fallback-data rule → `no-fallback-data.md`
- File size limits → `file-size-limits.md`
- Pre-commit verification → `compile-cleanly.md`
- Code-review duplication check → `code-cleanliness.md`
- Testing standards (BQ-public-data integration tests) → `testing.md`
- Multi-agent parallel work → `agents-and-swarm.md`
- 2026 Claude Code synthesis → `claude-code-best-practices.md`

### Conventions / naming / style
→ `docs/conventions/`
- Go style (errors, contexts, goroutines) → `go-style.md`
- Logging format + PII rules → `logging.md`
- Branch naming, commits → `git-workflow.md`
- Warehouse error → SQLSTATE table → `error-mapping.md`
- Customer namespacing, env var prefixes → `naming.md`

### Reference / requirements
→ `docs/reference/`
- Full product brief → `product-brief.md`
- Phase 1 build order → `phase-1-build-order.md`
- Env var catalogue → `env-vars.md`
- System requirements (Go 1.22+, CGo, Docker) → `requirements.md`

### Change tracking
→ `docs/changelog/`
- Architectural decisions → `CHANGELOG.md`
- Open observations → `IMPROVEMENTS.md`

## Resolution algorithm

1. Parse task keywords from user prompt.
2. Match against routing table.
3. Return ordered list (most relevant first).
4. Flag ambiguity; ask user to clarify when keywords match multiple subfolders.

## Maintenance

When a doc is added/renamed in `docs/`, update:
1. `docs/INDEX.md` (file listing)
2. `docs/CLAUDE.md` (folder map)
3. This skill's routing table
