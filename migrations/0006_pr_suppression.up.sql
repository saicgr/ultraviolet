-- Phase 5b — lineage-bot PR suppression. Records `/uv suppress` opt-outs so the
-- bot stops posting/editing the impact-analysis comment for a given PR.
CREATE TABLE pr_suppression (
    repo            TEXT        NOT NULL,
    pr_number       INT         NOT NULL,
    suppressed_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    suppressed_by   TEXT,
    PRIMARY KEY (repo, pr_number)
);
