-- Phase 3 + Phase 4 schema scaffolding. One migration so dev resets are atomic.
-- Each table is a thin first slice; column shape stable, business logic lives in
-- internal/<package>. See AUDIT.md round-3 batch fix.

-- W1 lineage: runtime edges. (upstream, downstream) plus optional column-level.
CREATE TABLE lineage_edge (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    upstream_fqn    TEXT NOT NULL,                       -- "schema.table" or "schema.table.col"
    downstream_fqn  TEXT NOT NULL,
    edge_type       TEXT NOT NULL CHECK (edge_type IN ('table','column')),
    query_hash      TEXT NOT NULL,
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    seen_count      BIGINT NOT NULL DEFAULT 1,
    UNIQUE (customer_id, upstream_fqn, downstream_fqn, edge_type)
);
CREATE INDEX idx_lineage_upstream ON lineage_edge(customer_id, upstream_fqn);
CREATE INDEX idx_lineage_downstream ON lineage_edge(customer_id, downstream_fqn);

-- W2 metadata: catalogue rows enriched from external systems (dbt, Tableau, Hex,
-- Looker, BQ INFORMATION_SCHEMA, Metaplane, Atlan).
CREATE TABLE table_metadata (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    fqn             TEXT NOT NULL,
    description     TEXT,
    owner           TEXT,
    sla_minutes     INT,
    tags            TEXT[],
    source          TEXT NOT NULL CHECK (source IN ('dbt','tableau','hex','looker','bq','metaplane','atlan','manual')),
    payload         JSONB,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (customer_id, fqn, source)
);
CREATE INDEX idx_table_metadata_fqn ON table_metadata(customer_id, fqn);

-- W3 catalog FTS: simple full-text search across description + fqn.
CREATE INDEX idx_table_metadata_fts
    ON table_metadata USING GIN (to_tsvector('english', coalesce(description, '') || ' ' || fqn));

-- W5 dashboards + semantic.
CREATE TABLE semantic_model (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    yaml_source     TEXT NOT NULL,
    compiled_sql    TEXT,
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (customer_id, name)
);

CREATE TABLE dashboard (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    layout          JSONB NOT NULL DEFAULT '{}'::jsonb,
    tiles           JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by      UUID,
    is_public       BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_dashboard_customer ON dashboard(customer_id);

-- W6 alerts: freshness + anomaly + dq.
CREATE TABLE alert_rule (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    rule_type       TEXT NOT NULL CHECK (rule_type IN ('freshness','anomaly','dq','schema_change')),
    target_fqn      TEXT,
    config          JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    last_fired_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Phase 4 RBAC: workspaces (sub-orgs within a customer) + members + roles.
CREATE TABLE workspace (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    slug            TEXT NOT NULL,
    name            TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (customer_id, slug)
);

CREATE TABLE app_user (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    email           TEXT NOT NULL,
    display_name    TEXT,
    sso_subject     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (customer_id, email)
);

CREATE TABLE workspace_membership (
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    role            TEXT NOT NULL CHECK (role IN ('admin','editor','viewer','auditor')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);

-- Phase 4 audit log (append-only; partitioned by month if/when volume warrants it).
CREATE TABLE audit_log (
    id              BIGSERIAL PRIMARY KEY,
    customer_id     UUID REFERENCES customers(id) ON DELETE SET NULL,
    user_id         UUID,
    action          TEXT NOT NULL,
    target_type     TEXT,
    target_id       TEXT,
    payload         JSONB,
    remote_ip       INET,
    user_agent      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_log_customer ON audit_log(customer_id, created_at DESC);
CREATE INDEX idx_audit_log_user ON audit_log(user_id, created_at DESC);

-- Phase 4 SSO config (per-customer SAML/OIDC).
CREATE TABLE sso_config (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id         UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    protocol            TEXT NOT NULL CHECK (protocol IN ('saml','oidc')),
    issuer              TEXT NOT NULL,
    metadata_url        TEXT,
    metadata_xml        TEXT,
    client_id           TEXT,
    client_secret_ct    BYTEA,
    client_secret_nonce BYTEA,
    enabled             BOOLEAN NOT NULL DEFAULT false,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (customer_id, protocol)
);

-- Phase 4 billing usage events.
CREATE TABLE usage_event (
    id              BIGSERIAL PRIMARY KEY,
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    event_type      TEXT NOT NULL,                    -- query|dashboard_render|ai_token|storage_gb
    quantity        NUMERIC(20,6) NOT NULL,
    metadata        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_usage_event_customer ON usage_event(customer_id, created_at DESC);

-- Phase 4 plan/billing.
CREATE TABLE plan_subscription (
    customer_id     UUID PRIMARY KEY REFERENCES customers(id) ON DELETE CASCADE,
    plan            TEXT NOT NULL CHECK (plan IN ('free','team','business','enterprise')),
    stripe_customer_id TEXT,
    stripe_sub_id   TEXT,
    monthly_cap_usd NUMERIC(10,2),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Reverse-ETL destination registry.
CREATE TABLE reverse_etl_destination (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    kind            TEXT NOT NULL,                    -- salesforce|hubspot|webhook|kafka|kinesis|...
    name            TEXT NOT NULL,
    config_ct       BYTEA NOT NULL,
    config_nonce    BYTEA NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Data quality test results.
CREATE TABLE dq_result (
    id              BIGSERIAL PRIMARY KEY,
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    table_fqn       TEXT NOT NULL,
    test_name       TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('pass','fail','skip','error')),
    details         JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_dq_result_table ON dq_result(customer_id, table_fqn, created_at DESC);
