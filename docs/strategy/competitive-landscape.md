# Competitive Landscape

## Direct competitors (warehouse cost optimization via routing/caching)

### Greybeam.ai
- **Pitch:** "Drop-in query engine for Snowflake."
- **Mechanism:** ADBC-native proxy; routes 80–98% of read queries to managed DuckDB clusters; queries fall back to Snowflake when ineligible.
- **Strength:** strong DuckDB engineering; ADBC-first means Arrow-throughput end-to-end; great blog content on DuckDB/Iceberg.
- **Weakness:** Snowflake-only. No BigQuery, no Databricks. No warehouse-agnostic AI SQL.
- **Pricing:** per-hour of compute.

### Keebo
- **Pitch:** Snowflake cost optimization via warehouse autosuspend / autoresize / query rewriting.
- **Mechanism:** sits in Snowflake account, manipulates warehouse parameters and rewrites queries; doesn't proxy.
- **Strength:** zero-network-path change; fast onboarding.
- **Weakness:** Snowflake-only; in-warehouse only — caps savings at ~30–50% (no offload to cheaper compute).
- **Pricing:** percentage of savings.

### Espresso AI
- **Pitch:** AI-powered Snowflake observability + query optimization.
- **Mechanism:** observability + query rewrite suggestions; some auto-rewrite.
- **Weakness:** primarily a recommendation engine; less aggressive cost cut than offload-to-DuckDB.

## Adjacent (not direct, but relevant)

### MotherDuck
- **Mechanism:** managed DuckDB SaaS; customers write to MotherDuck directly.
- **Why not a competitor:** requires data migration / dual-write. Ultraviolet is a transparent proxy, no migration.

### Materialize
- **Mechanism:** streaming materialized views.
- **Why not a competitor:** different problem (real-time streaming) vs. read-cache (Ultraviolet).

### dbt Cloud + dbt-snowflake
- **Mechanism:** ETL/ELT layer on top of warehouse.
- **Why not a competitor:** orthogonal — Ultraviolet sits between dbt and the warehouse and accelerates dbt's reads.

## Ultraviolet's wedge

Three claims, in order of strength:

1. **Multi-warehouse from day one.** Build for BigQuery + Snowflake + Databricks; launch with BigQuery (Phase 1) but announce Snowflake (Phase 2) credibly. No competitor does this — Greybeam is Snowflake-only, Keebo is Snowflake-only, MotherDuck doesn't proxy. **This is the primary positioning.**
2. **Warehouse-agnostic `ai_generate()`.** BigQuery has `AI.GENERATE_TEXT()` natively. Snowflake has `SNOWFLAKE.CORTEX.COMPLETE`. Databricks has model serving. Each is incompatible with the others. Ultraviolet's `ai_generate(prompt, model)` works across all warehouses — the same dbt model runs on any backend.
3. **Iceberg-as-shared-truth.** Customers who already standardize on Iceberg get zero-copy reads via either catalog passthrough or BYO mode (`docs/architecture/iceberg-modes.md`).

## Threat model

- **Greybeam adds BigQuery support.** Mitigation: ship BigQuery first, lock the AI rewriter as a differentiator, invest in dbt-package + Looker-block partnerships before they catch up.
- **Snowflake / Databricks bake offload-to-DuckDB natively.** Both have signaled interest. Mitigation: cross-warehouse layer is hard for either to build (they don't want to send data to the other). Lean into *cross-* warehouse as the moat.
- **Cloud providers (AWS Athena, GCP BigLake) close the gap.** Mitigation: Ultraviolet can run on top of Athena/BigLake rather than competing — they become a substrate.

## Sources

- https://www.greybeam.ai
- https://www.greybeam.ai/blog/snowflake-in-duckdb-adbc
- https://www.greybeam.ai/compare/keebo
- https://blog.greybeam.ai/iceberg-and-snowflake/
