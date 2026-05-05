# Architecture Overview

```
BI Tool / SQL Client (Looker, Tableau, dbt, psql, Hex)
        │
        │  Postgres wire (Phase 1) / ADBC (Phase 1.5) / Snowflake-wire (Phase 2)
        ▼
┌────────────────────────────────────────────────────────────┐
│             PROXY LAYER (Go, :5432 prod / :5000 dev)        │
│  internal/protocols/{pgwire,adbc,snowflake-wire}/          │
│  internal/router/  (DuckDB vs warehouse vs hybrid)         │
│  internal/ai/      (ai_generate rewriter — Path A / B)     │
└──────────┬──────────────────────────────┬──────────────────┘
           │                              │
   [cache hit / read-only]        [DDL / write / unsynced]
           ▼                              ▼
┌──────────────────────┐    ┌──────────────────────────────┐
│  DUCKDB WORKER POOL  │    │    WAREHOUSE CONNECTORS       │
│  internal/workers/   │    │    internal/connectors/       │
│  - DuckDB 1.x (CGo)  │    │    - BigQuery (Phase 1)       │
│  - Iceberg ext.      │    │    - Snowflake (Phase 1 stub) │
│  - LLM ext. (ai_gen) │    │    - Databricks (Phase 2)     │
│  - per-customer ns   │    │    Streams results back as PG │
└──────────┬───────────┘    │    DataRow frames             │
           │                └──────────────────────────────┘
           ▼
┌──────────────────────────────────────────────────────────┐
│                       SYNC LAYER                          │
│  internal/sync/  (CDC pollers per warehouse)             │
│  internal/iceberg/  (writer — DuckDB-Iceberg ext default)│
│  Three modes: sync / catalog passthrough / BYO Iceberg   │
└──────────┬───────────────────────────────────────────────┘
           ▼
┌──────────────────────────────────────────────────────────┐
│                    STORAGE LAYER                          │
│  Mode A (managed):  s3://uv-data/{customer}/{table}/     │
│  Mode B (BYOS):     customer S3/GCS via cross-acct IAM   │
└──────────────────────────────────────────────────────────┘
           ▲
           │
┌──────────────────────────────────────────────────────────┐
│             CONTROL PLANE (Go + React, :8080)             │
│  internal/api/   (chi router)                            │
│  internal/store/ (Postgres 16 + sqlc + golang-migrate)   │
│  frontend/       (React + TS + Tailwind + shadcn)        │
└──────────────────────────────────────────────────────────┘
```

## Component responsibilities

| Layer | Package | Responsibility |
|---|---|---|
| Protocol | `internal/protocols/*` | Wire-protocol parse/serialize; auth extraction |
| Router | `internal/router` | Classify SQL; pick DuckDB vs warehouse vs hybrid |
| AI rewriter | `internal/ai` | Detect + rewrite `ai_generate(...)`; Path A/B |
| DuckDB pool | `internal/workers` | Per-customer worker lifecycle; Iceberg attach |
| Connectors | `internal/connectors` | Warehouse SDK adapters; result streaming |
| Sync | `internal/sync` | CDC poll → Iceberg write → Redis pubsub refresh |
| Iceberg | `internal/iceberg` | Spec-conformant write; REST catalog |
| Cost | `internal/cost` | Per-warehouse cost APIs; backfill |
| API | `internal/api` | REST control plane |
| Store | `internal/store` | Postgres persistence (sqlc) |
| Config | `internal/config` | Env + YAML loader |
| Logger | `internal/logger` | zerolog wrapper; PII scrub |

See `client-protocols.md`, `routing.md`, `cdc-sync.md`, `iceberg.md`, `multi-warehouse.md` for per-component depth.
