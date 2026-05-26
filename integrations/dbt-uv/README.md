# dbt-uv — Ultraviolet adapter for dbt

A dbt adapter that materializes models directly into Iceberg via Ultraviolet,
so dbt runs become "compute via warehouse / compute via DuckDB" hybrid jobs
governed by the same router that powers BI traffic.

## Install

```bash
pip install dbt-uv
```

## profiles.yml

```yaml
my_project:
  outputs:
    dev:
      type: uv
      host: localhost
      port: 5000           # Ultraviolet PG-wire port
      user: my_customer_slug
      pass: uvk_...        # Ultraviolet API key
      dbname: my_customer_slug_bigquery
      schema: analytics
      threads: 4
  target: dev
```

## What it does

The adapter is a thin shim over `dbt-postgres`:

- **Wire protocol:** Postgres v3 (Ultraviolet implements it).
- **`materialized='table'`:** issues `CREATE TABLE … AS SELECT` against the proxy.
  The router sends the SELECT to the warehouse for full evaluation, then writes
  the result to Iceberg via the sync layer.
- **`materialized='incremental'`:** uses Ultraviolet's `synced_tables` watermark
  semantics — the dbt model registers as a watermark sync target.
- **`materialized='view'`:** registers a semantic-layer entry instead of a SQL
  view, so the dashboarding layer can resolve it through the semantic API.

## Status

This directory is the adapter scaffold. The Python package itself ships from a
separate PyPI repo; the `setup.py` + `dbt/adapters/uv/` tree generates from
this template. See `cmd/uv` for the CLI and `internal/api/openapi.yaml` for
the wire contract.
