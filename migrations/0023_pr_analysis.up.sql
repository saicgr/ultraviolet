-- W2 persisted PR impact analyses so the webapp can show PR history (impact +
-- data-quality references) per pull request. One row per analyzed head commit;
-- a re-push of the same SHA upserts (ON CONFLICT) rather than duplicating.
CREATE TABLE pr_analysis (
    id              BIGSERIAL   PRIMARY KEY,
    customer_id     UUID        NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    installation_id BIGINT,
    repo_full_name  TEXT        NOT NULL,
    pr_number       INT         NOT NULL,
    head_sha        TEXT        NOT NULL,
    pull_request_url TEXT       NOT NULL DEFAULT '',
    changes         JSONB       NOT NULL DEFAULT '[]',  -- detected impact.Change[]
    hits            JSONB       NOT NULL DEFAULT '[]',  -- impact.Hit[] (blast radius)
    dq_refs         JSONB       NOT NULL DEFAULT '[]',  -- {table_fqn,test_name,status}[]
    conclusion      TEXT        NOT NULL DEFAULT 'neutral', -- success|neutral|action_required
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repo_full_name, pr_number, head_sha)
);
CREATE INDEX idx_pr_analysis_repo_pr   ON pr_analysis(repo_full_name, pr_number, created_at DESC);
CREATE INDEX idx_pr_analysis_customer  ON pr_analysis(customer_id, created_at DESC);
