-- Phase 6 Wave 1: per-user $ quotas + cost-history index for inline query history.

CREATE TABLE user_quota (
    customer_id     UUID        NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    user_slug       TEXT        NOT NULL,
    monthly_cap_usd NUMERIC(10,2) NOT NULL,
    hard_block      BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (customer_id, user_slug)
);

CREATE INDEX IF NOT EXISTS idx_query_log_customer_cost
    ON query_log(customer_id, actual_cost_usd)
    WHERE actual_cost_usd IS NOT NULL;
