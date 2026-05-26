-- pu-1 + pu-6: scheduled query execution history + outbound webhook registry.
-- scheduled_query_run records each execution of a scheduled_query row so the UI
-- can render a run history. webhook_endpoint stores customer-registered HTTPS
-- targets for sync/dashboard/agent events; the dispatcher signs payloads with
-- the per-endpoint secret (HMAC-SHA256). No retry / dead-letter yet — that
-- lands in a follow-up migration once we wire actual call sites.

CREATE TABLE scheduled_query_run (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scheduled_query_id    UUID NOT NULL REFERENCES scheduled_query(id) ON DELETE CASCADE,
    started_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at           TIMESTAMPTZ,
    status                TEXT NOT NULL CHECK (status IN ('running','success','error')),
    error_message         TEXT,
    rows_returned         INT
);
CREATE INDEX idx_scheduled_query_run_query ON scheduled_query_run(scheduled_query_id, started_at DESC);

-- scheduled_query needs a next_run_at column for the runner to pick rows up.
-- Existing rows default to now() so they execute on the first tick.
ALTER TABLE scheduled_query ADD COLUMN IF NOT EXISTS next_run_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE TABLE webhook_endpoint (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id         UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    url                 TEXT NOT NULL,
    event_kinds         TEXT[] NOT NULL DEFAULT '{}',
    secret_encrypted    BYTEA NOT NULL,
    active              BOOLEAN NOT NULL DEFAULT true,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_webhook_endpoint_customer ON webhook_endpoint(customer_id, active);
