-- Phase 6 — user-focused features. Saved queries, annotations, notifications,
-- email subscriptions, scheduled queries, public-share tokens, cost budgets,
-- PII tags, activity feed.

CREATE TABLE saved_query (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    user_id         UUID,
    name            TEXT NOT NULL,
    description     TEXT,
    sql             TEXT NOT NULL,
    params          JSONB NOT NULL DEFAULT '{}'::jsonb,    -- ${start_date} etc
    favorite        BOOLEAN NOT NULL DEFAULT false,
    last_run_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (customer_id, user_id, name)
);
CREATE INDEX idx_saved_query_user ON saved_query(customer_id, user_id, favorite, updated_at DESC);

-- Comments / annotations on dashboards, tiles, lineage nodes, query results.
CREATE TABLE annotation (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    user_id         UUID,
    target_type     TEXT NOT NULL CHECK (target_type IN ('dashboard','tile','lineage_node','query','table','metric')),
    target_id       TEXT NOT NULL,
    body            TEXT NOT NULL,
    pinned_at_event TIMESTAMPTZ,                            -- e.g. annotate the value at a point in time
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_annotation_target ON annotation(customer_id, target_type, target_id, created_at DESC);

-- In-app notifications inbox: alerts, mentions, sync failures, agent suggestions.
CREATE TABLE notification (
    id              BIGSERIAL PRIMARY KEY,
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    user_id         UUID,                                   -- nullable = workspace-wide
    kind            TEXT NOT NULL,                          -- mention | alert | agent | sync_fail | dashboard_share
    title           TEXT NOT NULL,
    body            TEXT,
    link            TEXT,
    read_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_notification_inbox ON notification(customer_id, user_id, read_at NULLS FIRST, created_at DESC);

-- Email / chatops subscription for a dashboard or saved query: recurring snapshot.
CREATE TABLE delivery_subscription (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    user_id         UUID,
    target_type     TEXT NOT NULL CHECK (target_type IN ('dashboard','saved_query')),
    target_id       UUID NOT NULL,
    schedule_cron   TEXT NOT NULL,                          -- "0 9 * * 1" = Mon 9am
    channel         TEXT NOT NULL CHECK (channel IN ('email','slack','teams','webhook')),
    address         TEXT NOT NULL,                          -- email, slack-channel, webhook URL
    enabled         BOOLEAN NOT NULL DEFAULT true,
    last_sent_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Scheduled SQL: run nightly, append to a table or send a snapshot.
CREATE TABLE scheduled_query (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    sql             TEXT NOT NULL,
    schedule_cron   TEXT NOT NULL,
    target_table    TEXT,                                   -- if non-null, append result rows here
    enabled         BOOLEAN NOT NULL DEFAULT true,
    last_run_at     TIMESTAMPTZ,
    last_run_status TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Public-share token for dashboards. Customer-revocable.
CREATE TABLE share_token (
    token           TEXT PRIMARY KEY,                       -- random 24-byte hex
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    target_type     TEXT NOT NULL CHECK (target_type IN ('dashboard','saved_query')),
    target_id       UUID NOT NULL,
    expires_at      TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Per-workspace cost budget + soft / hard caps with alert hooks.
CREATE TABLE cost_budget (
    customer_id     UUID PRIMARY KEY REFERENCES customers(id) ON DELETE CASCADE,
    monthly_cap_usd NUMERIC(10,2) NOT NULL,
    soft_pct        INT NOT NULL DEFAULT 80,                -- warn at 80%
    hard_pct        INT NOT NULL DEFAULT 100,               -- block at 100%
    alert_channel   TEXT,                                   -- "slack:#data-finops"
    last_warn_sent  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Column-level PII tags. Auto-populated by the PII tagger agent.
CREATE TABLE column_tag (
    id              BIGSERIAL PRIMARY KEY,
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    table_fqn       TEXT NOT NULL,
    column_name     TEXT NOT NULL,
    tag             TEXT NOT NULL,                          -- pii.email | pii.ssn | pii.phone | financial | hipaa | ...
    confidence      NUMERIC(3,2) NOT NULL DEFAULT 1.0,      -- 0..1
    source          TEXT NOT NULL DEFAULT 'manual',         -- manual | auto | regex | llm
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (customer_id, table_fqn, column_name, tag)
);
CREATE INDEX idx_column_tag_table ON column_tag(customer_id, table_fqn);

-- Activity feed: workspace-wide who-did-what (read by Inbox + activity page).
CREATE TABLE activity_event (
    id              BIGSERIAL PRIMARY KEY,
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    user_id         UUID,
    kind            TEXT NOT NULL,                          -- dashboard.created | query.saved | sync.completed | ...
    target_type     TEXT,
    target_id       TEXT,
    summary         TEXT NOT NULL,
    payload         JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_activity_event_feed ON activity_event(customer_id, created_at DESC);

-- Query result cache (per-user, ephemeral). Lets us export a result without re-running.
CREATE TABLE query_result_cache (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    user_id         UUID,
    query_hash      TEXT NOT NULL,
    rows_jsonb      JSONB NOT NULL,
    row_count       INT NOT NULL,
    bytes           BIGINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_query_result_cache_expiry ON query_result_cache(expires_at);
