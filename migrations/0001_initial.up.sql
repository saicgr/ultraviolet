-- Ultraviolet control-plane schema. See docs/architecture/cdc-sync.md and product-brief.md §8.
-- Per docs/process/no-fallback-data.md: every error path must surface; no silent recovery.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE customers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug            TEXT UNIQUE NOT NULL,
    display_name    TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Per-customer warehouse credentials. credentials_ciphertext is AES-256-GCM
-- encrypted JSON; nonce stored alongside (12 bytes). Plaintext is forbidden.
CREATE TABLE connections (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id             UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    warehouse_type          TEXT NOT NULL CHECK (warehouse_type IN ('bigquery','snowflake','databricks')),
    name                    TEXT NOT NULL,
    credentials_ciphertext  BYTEA NOT NULL,
    credentials_nonce       BYTEA NOT NULL,
    storage_mode            TEXT NOT NULL DEFAULT 'managed' CHECK (storage_mode IN ('managed','byos')),
    s3_bucket               TEXT,
    s3_prefix               TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (customer_id, name)
);
CREATE INDEX idx_connections_customer ON connections(customer_id);

CREATE TABLE api_keys (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    key_hash        BYTEA NOT NULL UNIQUE,         -- sha256(api_key)
    key_prefix      TEXT NOT NULL,                  -- first 8 chars for display
    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at    TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ
);
CREATE INDEX idx_api_keys_customer ON api_keys(customer_id) WHERE revoked_at IS NULL;
CREATE INDEX idx_api_keys_hash ON api_keys(key_hash) WHERE revoked_at IS NULL;

CREATE TABLE synced_tables (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id       UUID NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    schema_name         TEXT NOT NULL,
    table_name          TEXT NOT NULL,
    sync_mode           TEXT NOT NULL CHECK (sync_mode IN ('cdc_native','watermark','manual')),
    watermark_column    TEXT,
    last_sync_at        TIMESTAMPTZ,
    last_snapshot_id    BIGINT,
    last_watermark      TEXT,                       -- opaque per warehouse
    iceberg_path        TEXT,
    row_count           BIGINT NOT NULL DEFAULT 0,
    sync_lag_seconds    INT,
    state               TEXT NOT NULL DEFAULT 'pending'
                        CHECK (state IN ('pending','initial_load','live','error','paused')),
    last_error          TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (connection_id, schema_name, table_name)
);
CREATE INDEX idx_synced_tables_state ON synced_tables(state) WHERE state IN ('pending','initial_load');

CREATE TABLE sync_jobs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    synced_table_id     UUID NOT NULL REFERENCES synced_tables(id) ON DELETE CASCADE,
    job_type            TEXT NOT NULL CHECK (job_type IN ('initial_load','incremental','backfill')),
    state               TEXT NOT NULL DEFAULT 'queued'
                        CHECK (state IN ('queued','running','succeeded','failed','cancelled')),
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    rows_processed      BIGINT NOT NULL DEFAULT 0,
    bytes_processed     BIGINT NOT NULL DEFAULT 0,
    snapshot_id         BIGINT,
    error_message       TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sync_jobs_table ON sync_jobs(synced_table_id, started_at DESC);

CREATE TABLE query_log (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id         UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    connection_id       UUID REFERENCES connections(id) ON DELETE SET NULL,
    api_key_id          UUID REFERENCES api_keys(id) ON DELETE SET NULL,
    query_hash          TEXT NOT NULL,
    normalized_sql      TEXT NOT NULL,                -- literals stripped (logger.NormalizeSQL)
    route_decision      TEXT NOT NULL CHECK (route_decision IN ('duckdb','warehouse','ai_rewrite','fallback','error')),
    fallback_reason     TEXT,
    duration_ms         INT NOT NULL,
    rows_returned       BIGINT NOT NULL DEFAULT 0,
    bytes_scanned       BIGINT,
    estimated_cost_usd  NUMERIC(12,6),
    actual_cost_usd     NUMERIC(12,6),
    error_code          TEXT,
    started_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_query_log_customer_started ON query_log(customer_id, started_at DESC);
CREATE INDEX idx_query_log_route ON query_log(route_decision, started_at DESC);
CREATE INDEX idx_query_log_actual_cost_null ON query_log(started_at) WHERE actual_cost_usd IS NULL;

CREATE TABLE cost_attribution (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id             UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    connection_id           UUID NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    period_start            TIMESTAMPTZ NOT NULL,
    period_end              TIMESTAMPTZ NOT NULL,
    warehouse_cost_usd      NUMERIC(12,6) NOT NULL DEFAULT 0,
    duckdb_cost_usd         NUMERIC(12,6) NOT NULL DEFAULT 0,
    estimated_savings_usd   NUMERIC(12,6) NOT NULL DEFAULT 0,
    queries_total           BIGINT NOT NULL DEFAULT 0,
    queries_duckdb          BIGINT NOT NULL DEFAULT 0,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (customer_id, connection_id, period_start)
);
CREATE INDEX idx_cost_attribution_customer ON cost_attribution(customer_id, period_start DESC);
